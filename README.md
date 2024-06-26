# GitLab Recurring Issues

A Docker image for creating recurring issues based on templates as part of a
GitLab scheduled pipeline.

Forked from ⭐
[ph1ll/gitlab-recurring-issues](https://github.com/ph1ll/gitlab-recurring-issues)
and adapted for my personal use.

The Docker image is available on
[DockerHub](https://hub.docker.com/repository/docker/tamslo/gitlab-issue-automation).

_⚠️ Please note that I did not add any unit testing; so use with caution –_
_especially added features like n-weekly occurrence, exceptions, adapting board_
_labels, and creating standup meeting notes._

## Usage

Create template issues in the `.gitlab/recurring_issue_templates/` directory as
Markdown files.
Template issues use YAML front matter for configuration settings.
The template body is used as the issue description.

```markdown
---
title: "Biweekly reminder" # The issue title
labels: ["important", "to do"] # Optional; list of labels (will be created if not present)
confidential: false # Optional; defines visibility of issue (default for bool in Go is false)
duein: "24h" # Optional; time to due date from `crontab` as per https://pkg.go.dev/time?tab=doc#ParseDuration (e.g "30m", "1h")
crontab: "@weekly" # The recurrance schedule for issue creation using crontab syntax
weeklyRecurrence: 2 # Optional; if stated, the `crontab` condition will only be applied to every n-th week, based on titles of present issues
---
(**You need to give a description, otherwise parsing will fail!**)

This is your biweekly reminder to perform the following actions:

* [ ] Action 1
* [ ] Action 2
```

Create a pipeline in the `.gitlab-ci.yml` file:

```yaml
recurring issues:
  image: tamslo/gitlab-recurring-issues
  script: gitlab-recurring-issues
  only: 
    - schedules
```

Create project CI/CD variables:

| Name | Value |
| ---- | ----- |
| GITLAB_API_TOKEN | The API access token for the user account that will create the issues (see: [GitLab docs](https://docs.gitlab.com/ce/user/profile/personal_access_tokens.html)) |
| GROUP_WIKI_ID | Optional. Set wiki for standup notes to a group wiki instead of the current project's wiki. |
| FORCE_STANDUP_NOTES_FOR_TODAY | Optional. Force the creation of standup notes for today if setting to `TRUE`. |

Finally, create a new schedule under the project CI/CD options, ensuring that
the pipeline runs at least as often as your most frequent job.

### Adding Recurrance Exceptions

To add exceptions to recurrances, create a file named
`recurrance_exceptions.yml` in the templates folder.
Note that exception dates are applied to the creation date given in `crontab`,
not the due date.

It can contain exception definitions and rules that map issues by their IDs
(need to be given in the issue template) to exception definitions.

Start and end dates are given in the format `YYYY-MM-DD`.
If an exception occurs every year, the placeholder `YEAR` can be given (needs to
be set for both `start` and `end`).

```yaml
definitions:
  -
    id: "christmas-break"
    start: "YEAR-12-24"
    end: "YEAR-01-01"
  -
    id: "vacation"
    start: "2022-05-13"
    end: "2022-05-20"
  -
    id: "no-meeting"
    start: "2022-04-20"
    end: "2022-04-20"
rules:
  -
    issue: "weekly-meeting"
    exceptions: ["christmas-break", "vacation", "no-meeting"]
```

### Automatically Moving Issues on Board

The script also checks whether labels for custom issue management on a board
view exist (see label definitions in `constants` and
`board_labels/board_labels.go`).

If an issue is due, the `TodayLabel` or `ThisWeekLabel` will be added if it is
not present and no `OtherLabels` exist that indicate that the issue is in
progress.
If the `TodayLabel` is added and the `ThisWeekLabel` present, the latter will be
removed.

### Add Standup Notes

A helper will create standup meeting notes on the day of the `prepare-standup`
recurring issue, if the issue exists and no notes exist yet.

All issues that were updated between the last standup meeting note and the
current one that were not closed before the last standup note will be included
as a list to help filling out the table (included as template).

By default, the wiki page is created in the wiki of the current project.
If the CI/CD variable `GROUP_WIKI_ID` is set to the ID of a group, the page will
be created in the wiki of this group.

### Issue for Vacation Start

A special recurring issue can be defined in the issue template `vacation.md`.
This issue is created on the last workday before an upcoming vacation
(identified as exceptions with IDs starting with `vacation-`).

_⚠️ Not implemented yet, see_
_[#8](https://github.com/tamslo/gitlab-issue-automation/issues/8)._
