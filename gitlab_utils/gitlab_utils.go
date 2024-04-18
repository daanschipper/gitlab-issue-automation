package gitlabUtils

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	constants "gitlab-issue-automation/constants"
	types "gitlab-issue-automation/types"

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
		Name:                  "GITLAB_API_TOKEN",
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

func GetCiJobName() string {
	return GetEnvVariable(&envVariableParameters{Name: "CI_JOB_NAME"})
}

func GetGroupWikiId() string {
	return GetEnvVariable(&envVariableParameters{Name: "GROUP_WIKI_ID", Optional: true})
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
	lastRunTime := time.Unix(0, 0)
	options := &gitlab.ListProjectPipelinesOptions{
		Scope:   gitlab.String("finished"),
		Status:  gitlab.BuildState(gitlab.Success),
		OrderBy: gitlab.String("updated_at"),
	}
	ciProjectID := GetCiProjectId()
	ciJobName := GetCiJobName()
	pipelineInfos, _, err := git.Pipelines.ListProjectPipelines(ciProjectID, options)
	if err != nil {
		log.Fatal(err)
	}
	for _, pipelineInfo := range pipelineInfos {
		jobs, _, err := git.Jobs.ListPipelineJobs(ciProjectID, pipelineInfo.ID, nil)
		if err != nil {
			log.Fatal(err)
		}
		for _, job := range jobs {
			if job.Name == ciJobName {
				lastRunTime = *job.FinishedAt
				return lastRunTime
			}
		}
	}
	return lastRunTime
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
	options := &gitlab.CreateIssueOptions{
		Title:        gitlab.String(data.Title),
		Description:  gitlab.String(data.Description),
		Confidential: &data.Confidential,
		CreatedAt:    &data.NextTime,
		Labels:       gitlab.Labels{strings.Join(append(data.Labels, "🔁 Recurring"), ",")},
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
		_, _, err = git.Wikis.GetWikiPage(project.ID, title)
	} else {
		_, _, err = git.GroupWikis.GetGroupWikiPage(groupWikiId, title)
	}
	return err == nil
}

func GetWikiPagesMetadata() []types.WikiMetadata {
	git := GetGitClient()
	groupWikiId := GetGroupWikiId()
	wikiMetadata := []types.WikiMetadata{}
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
