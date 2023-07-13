// Copyright 2021 Authors of Cilium
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package github

import (
	"context"
	"fmt"
	"regexp"

	"github.com/cilium/github-actions/pkg/jenkins"
	gh "github.com/google/go-github/v50/github"
)

type MLHCommand string

const (
	MLHCommandNewFlake MLHCommand = "/mlh new-flake "
	MLHCommandNotFound MLHCommand = ""
)

var (
	regexNewFlake = regexp.MustCompile(`^` + string(MLHCommandNewFlake))
)

// CommentAndOpenIssue creates a comment and (re-)opens a GH issue in case it is
// closed.
func (c *Client) CommentAndOpenIssue(ctx context.Context, owner, repo string, issueNumber int, body string) error {
	_, _, err := c.GHClient.Issues.CreateComment(ctx, owner, repo, issueNumber, &gh.IssueComment{
		Body: &body,
	})
	if err != nil {
		return err
	}

	_, _, err = c.GHClient.Issues.Edit(ctx, owner, repo, issueNumber, &gh.IssueRequest{
		State: func() *string { a := "open"; return &a }(),
	})

	return err
}

// CreateIssue creates a new GH issue. Returns the issue number created.
func (c *Client) CreateIssue(ctx context.Context, owner, repo string, title, body string, labels []string) (int, error) {
	ghIssue, _, err := c.GHClient.Issues.Create(ctx, owner, repo, &gh.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labels,
	})
	if err != nil {
		return 0, err
	}
	return ghIssue.GetNumber(), err
}

func IsMLHCommand(s string) MLHCommand {
	switch {
	case regexNewFlake.MatchString(s):
		return MLHCommandNewFlake
	default:
		return MLHCommandNotFound
	}
}

func (c *Client) HandleIssueCommentEvent(ctx context.Context, cfg *FlakeConfig, jobName string, pr *gh.PullRequest, event *gh.IssueCommentEvent) error {
	prNumber := event.GetIssue().GetNumber()
	prURLFails, err := c.GetPRFailure(ctx, pr)
	if err != nil {
		return err
	}
	if len(prURLFails) == 0 {
		c.Log().Info().Fields(map[string]interface{}{"pr-number": prNumber}).Msg("PR without failures or is in draft")
		return nil
	}

	var failedJobNumber int64

	for _, prURLFail := range prURLFails {
		prJobName, jobNumber := jenkins.SplitJobNameNumber(prURLFail)
		if prJobName == jobName {
			failedJobNumber = jobNumber
		}
	}
	if failedJobNumber == 0 {
		return fmt.Errorf("job %q not found for PR %d: %s", jobName, prNumber, prURLFails)
	}

	jc, err := jenkins.NewJenkinsClient(ctx, cfg.JenkinsConfig.JenkinsURL, false)
	if err != nil {
		return err
	}
	_, jobFailures, err := jc.GetJobFailure(ctx, jobName, failedJobNumber)
	if err != nil {
		return err
	}
	var issueNumbers []int
	if len(jobFailures) == 0 {
		return fmt.Errorf("job #%d of %q had 0 failures / flakes", failedJobNumber, jobName)
	}

	var comment string

	// Do not create more GH issues than the ones specified by 'MaxFlakesPerTest'
	if len(jobFailures) < cfg.MaxFlakesPerTest {
		for _, jobFailure := range jobFailures {
			// Generate GH Issue comment
			title, body, err := jenkins.GHIssueDescription(jobFailure)
			if err != nil {
				return fmt.Errorf("unable to generate GH issue description: %w", err)
			}

			// Create a GH issue
			issueNumber, err := c.CreateIssue(ctx, c.orgName, c.repoName, title, body, cfg.IssueTracker.IssueLabels)
			if err != nil {
				return fmt.Errorf("unable to create GH issue: %w", err)
			}
			issueNumbers = append(issueNumbers, issueNumber)
		}

		comment, err = jenkins.PRCommentNewGHIssues(issueNumbers)
		if err != nil {
			return err
		}
	} else {
		comment = fmt.Sprintf(":-1: Unable to create GH issues: "+
			"number of flakes (%d) exceeds the maximum permited (%d).",
			len(jobFailures), cfg.MaxFlakesPerTest)
	}

	err = c.CreateOrAppendCommentIssueComment(ctx, prNumber, event.GetComment(), comment)
	if err != nil {
		return fmt.Errorf("unable to edit or create comment in PR %d", prNumber)
	}

	c.Log().Info().Fields(map[string]interface{}{"pr-number": prNumber}).Msg("Created GH issues for PR")
	return nil
}
