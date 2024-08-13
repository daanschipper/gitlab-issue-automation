package gitlabUtils

import (
	"crypto/tls"
	"gitlab-issue-automation/constants"
	types "gitlab-issue-automation/types"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/xanzy/go-gitlab"
)

type envVariableParameters struct {
	Name                  string
	ErrorMessageOverwrite string
	Optional              bool
}

func GetEnvVariable(parameters *envVariableParameters) string {
	envVariable := os.Getenv(parameters.Name)
	if envVariable == "" && !parameters.Optional {
		errorMessage := "This tool must be ran as part of a GitLab pipeline."
		if parameters.ErrorMessageOverwrite != "" {
			errorMessage = parameters.ErrorMessageOverwrite
		}
		log.Fatalf("Environment variable '%s' not found. %s", parameters.Name, errorMessage)
	}
	return envVariable
}

func GetGitlabAPIToken() string {
	return GetEnvVariable(&envVariableParameters{
		Name:                  "GITLAB_ISSUE_AUTOMATION_API_TOKEN",
		ErrorMessageOverwrite: "Ensure this is set under the project CI/CD settings.",
	})
}

func GetCiProjectId() string {
	return GetEnvVariable(&envVariableParameters{Name: "CI_PROJECT_ID"})
}

func GetCiAPIV4URL() string {
	return GetEnvVariable(&envVariableParameters{Name: "CI_API_V4_URL"})
}

func GetCiProjectDir() string {
	return GetEnvVariable(&envVariableParameters{Name: "CI_PROJECT_DIR"})
}

func GetGroupWikiId() string {
	return GetEnvVariable(&envVariableParameters{Name: "GROUP_WIKI_ID", Optional: true})
}

func GetScheduledPipelineId() int {
	pipelineId := GetEnvVariable(&envVariableParameters{
		Name: "RECURRING_TASKS_SCHEDULED_PIPELINE_ID",
	})

	atoi, err := strconv.Atoi(pipelineId)
	if err != nil {
		log.Fatal(err)
	}

	return atoi
}

func GetForceStandupNotesForToday() bool {
	variable := GetEnvVariable(&envVariableParameters{Name: "FORCE_STANDUP_NOTES_FOR_TODAY", Optional: true})
	return variable == "TRUE"
}

func GetGitClient() *gitlab.Client {
	transCfg := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{
		Transport: transCfg,
	}
	git, err := gitlab.NewClient(GetGitlabAPIToken(), gitlab.WithBaseURL(GetCiAPIV4URL()), gitlab.WithHTTPClient(httpClient))
	if err != nil {
		log.Fatal(err)
	}
	return git
}

func GetGitProject() *gitlab.Project {
	git := GetGitClient()
	project, _, err := git.Projects.GetProject(GetCiProjectId(), nil)
	if err != nil {
		log.Fatal(err)
	}
	return project
}

func GetRecurringIssuesPath() string {
	return path.Join(GetCiProjectDir(), constants.IssueTemplatePath)
}

func GetLastRunTime() time.Time {
	git := GetGitClient()
	ciProjectID := GetCiProjectId()
	ciScheduledPipelineId := GetScheduledPipelineId()

	schedule, _, err := git.PipelineSchedules.GetPipelineSchedule(ciProjectID, ciScheduledPipelineId)
	if err != nil {
		log.Fatal(err)
	}

	// Find latest successful pipeline, with pagination.
	// Need to loop through all pipelines, only since 17.2 sorting has been introduced https://gitlab.com/gitlab-org/gitlab/-/issues/37246.
	// Cannot use schedule.LastPipeline as the status can be failed.
	pipelinesTriggeredByScheduleOptions := gitlab.ListPipelinesTriggeredByScheduleOptions{
		Page: 1, PerPage: 10, Sort: "desc",
	}

	lastSuccessfulPipeline := time.Unix(0, 0)

	for {
		pipelines, pipelinesTriggeredByScheduleResponse, err := git.PipelineSchedules.ListPipelinesTriggeredBySchedule(ciProjectID, schedule.ID, &pipelinesTriggeredByScheduleOptions)
		if err != nil {
			log.Fatal(err)
		}

		for _, pipeline := range pipelines {
			if pipeline.Status == "success" {
				lastSuccessfulPipeline = *pipeline.CreatedAt
			}
		}

		if pipelinesTriggeredByScheduleResponse.NextPage == 0 {
			break
		}
		pipelinesTriggeredByScheduleOptions.Page = pipelinesTriggeredByScheduleResponse.NextPage
	}

	return lastSuccessfulPipeline
}

func GetSortedProjectIssues(orderBy string, sortOrder string, issueState string) []*gitlab.Issue {
	git := GetGitClient()
	project := GetGitProject()
	perPage := 20
	page := 1
	lastPageReached := false
	var issues []*gitlab.Issue
	for {
		if lastPageReached {
			break
		}
		listOptions := &gitlab.ListOptions{
			PerPage: perPage,
			Page:    page,
		}
		options := &gitlab.ListProjectIssuesOptions{
			OrderBy:     &orderBy,
			Sort:        &sortOrder,
			ListOptions: *listOptions,
		}
		if issueState != "" {
			options.State = &issueState
		}
		pageIssues, _, err := git.Issues.ListProjectIssues(project.ID, options)
		if err != nil {
			log.Fatal(err)
		}
		issues = append(issues, pageIssues...)
		if len(pageIssues) < perPage {
			lastPageReached = true
		} else {
			page++
		}
	}
	return issues
}

func GetIssueDueDate(data *types.Metadata) time.Time {
	duration, err := time.ParseDuration(data.DueIn)
	if err != nil {
		log.Fatal(err)
	}
	return data.NextTime.Add(duration)
}

func CreateIssue(data *types.Metadata) error {
	git := GetGitClient()
	project := GetGitProject()

	labelOptions := gitlab.LabelOptions(append(data.Labels, "🔁 Recurring"))

	options := &gitlab.CreateIssueOptions{
		Title:        gitlab.Ptr(data.Title),
		Description:  gitlab.Ptr(data.Description),
		Confidential: &data.Confidential,
		CreatedAt:    &data.NextTime,
		Labels:       &labelOptions,
	}
	if data.DueIn != "" {
		dueDate := gitlab.ISOTime(GetIssueDueDate(data))
		options.DueDate = &dueDate
	}
	_, _, err := git.Issues.CreateIssue(project.ID, options)
	if err != nil {
		return err
	}
	return nil
}

func UpdateIssue(issueId int, options *gitlab.UpdateIssueOptions) *gitlab.Issue {
	git := GetGitClient()
	project := GetGitProject()
	updatedIssue, _, err := git.Issues.UpdateIssue(project.ID, issueId, options)
	if err != nil {
		log.Fatal(err)
	}
	return updatedIssue
}

func WikiPageExists(title string) bool {
	git := GetGitClient()
	groupWikiId := GetGroupWikiId()
	var err error
	if groupWikiId == "" {
		project := GetGitProject()
		_, _, err = git.Wikis.GetWikiPage(project.ID, title, &gitlab.GetWikiPageOptions{})
	} else {
		_, _, err = git.GroupWikis.GetGroupWikiPage(groupWikiId, title, &gitlab.GetGroupWikiPageOptions{})
	}
	return err == nil
}

func GetWikiPagesMetadata() []types.WikiMetadata {
	git := GetGitClient()
	groupWikiId := GetGroupWikiId()
	var wikiMetadata []types.WikiMetadata
	if groupWikiId == "" {
		project := GetGitProject()
		options := &gitlab.ListWikisOptions{}
		wikiPages, _, err := git.Wikis.ListWikis(project.ID, options)
		if err != nil {
			log.Fatal(err)
		}
		for _, wikiPage := range wikiPages {
			wikiMetadata = append(wikiMetadata, types.WikiMetadata{Title: wikiPage.Title, Slug: wikiPage.Slug})
		}
	} else {
		options := &gitlab.ListGroupWikisOptions{}
		wikiPages, _, err := git.GroupWikis.ListGroupWikis(groupWikiId, options)
		if err != nil {
			log.Fatal(err)
		}
		for _, wikiPage := range wikiPages {
			wikiMetadata = append(wikiMetadata, types.WikiMetadata{Title: wikiPage.Title, Slug: wikiPage.Slug})
		}
	}
	return wikiMetadata
}

func CreateWikiPage(title string, content string) {
	git := GetGitClient()
	groupWikiId := GetGroupWikiId()
	format := gitlab.WikiFormatValue("markdown")
	var err error
	if groupWikiId == "" {
		project := GetGitProject()
		options := &gitlab.CreateWikiPageOptions{
			Content: &content,
			Title:   &title,
			Format:  &format,
		}
		_, _, err = git.Wikis.CreateWikiPage(project.ID, options)
	} else {
		options := &gitlab.CreateGroupWikiPageOptions{
			Content: &content,
			Title:   &title,
			Format:  &format,
		}
		_, _, err = git.GroupWikis.CreateGroupWikiPage(groupWikiId, options)
	}
	if err != nil {
		log.Fatal(err)
	}
}
