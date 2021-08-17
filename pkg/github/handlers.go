// Copyright 2019-2021 Authors of Cilium
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
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cilium/github-actions/pkg/jenkins"
	gh "github.com/google/go-github/v38/github"
)

type PRBlockerConfig struct {
	Project                      `yaml:",inline"`
	MoveToProjectsForLabelsXORed map[string]map[string]Project `yaml:"move-to-projects-for-labels-xored,omitempty"`
	RequireMsgsInCommit          []MsgInCommit                 `yaml:"require-msgs-in-commit,omitempty"`
	AutoLabel                    []string                      `yaml:"auto-label,omitempty"`
	BlockPRWith                  BlockPRWith                   `yaml:"block-pr-with,omitempty"`
	AutoMerge                    AutoMerge                     `yaml:"auto-merge,omitempty"`
	FlakeTracker                 *FlakeConfig                  `yaml:"flake-tracker,omitempty"`
}

func (c *Client) HandlePRE(cfg PRBlockerConfig, pre *gh.PullRequestEvent) error {
	pr := pre.GetPullRequest()
	owner := pr.Base.Repo.GetOwner().GetLogin()
	repoName := *pr.Base.Repo.Name
	prNumber := pr.GetNumber()
	action := pre.GetAction()
	c.log.Info().Fields(map[string]interface{}{
		"action":    action,
		"pr-number": prNumber,
	}).Msg("Action triggered from PR")

	// Assign individual reviewers to list of assignees
	if action == "review_requested" {
		users := pre.PullRequest.RequestedReviewers
		err := c.Assign(context.TODO(), owner, repoName, prNumber, users)
		if err != nil {
			c.log.Info().Fields(map[string]interface{}{
				"error":     err,
				"pr-number": prNumber,
			}).Msg("Unable to add assignees to PR")
			return err
		}
	}

	c.prLabels = ParseGHLabels(pr.Labels)

	// Autolabel PRs as soon they are created
	if len(cfg.AutoLabel) != 0 { // We only auto-label PRs if when they are open / reopen
		if action == "opened" || action == "reopened" {
			err := c.AutoLabel(cfg.AutoLabel, owner, repoName, prNumber)
			if err != nil {
				return err
			}
		}
	}

	// Check for msgs in commits
	if len(cfg.RequireMsgsInCommit) != 0 {
		if pr.GetState() != "closed" {
			switch action {
			case "opened", "reopened", "synchronize":
				err := c.CommitContains(cfg.RequireMsgsInCommit, owner, repoName, prNumber)
				if err != nil {
					return err
				}
			}
		}
	}

	// Block PRs if they miss or have particular labels set.
	if len(cfg.BlockPRWith.LabelsUnset) != 0 || len(cfg.BlockPRWith.LabelsSet) != 0 {
		if pr.GetState() != "closed" {
			switch action {
			case "labeled", "unlabeled", "synchronize", "opened", "reopened":
				blockPR, blockReasons, err := c.BlockPRWith(cfg.BlockPRWith, owner, repoName, prNumber)
				if err != nil {
					return err
				}
				// Update the mergeability checker
				err = c.UpdateMergeabilityCheck(owner, repoName, prNumber, pr.GetHead(), blockPR, blockReasons)
				if err != nil {
					return err
				}
			}
		}
	}

	// Put PR in projects for release tracking
	if len(cfg.Project.ColumnName) != 0 && len(cfg.Project.ProjectName) != 0 {
		if action == "opened" {
			err := c.PutPRInProject(owner, repoName, pr.GetID(), cfg.Project)
			if err != nil {
				// Ignore the error if the project was not found. It might mean
				// the project was closed so we don't need to track this PR on
				// it.
				if !errors.Is(err, &ErrProjectNotFound{projectName: cfg.Project.ProjectName}) {
					return err
				}
			}
		}
	}

	// Put PR in projects for backport release tracking
	if len(cfg.MoveToProjectsForLabelsXORed) != 0 {
		switch action {
		case "labeled", "unlabeled":
			err := c.SyncPRProjects(cfg.MoveToProjectsForLabelsXORed, owner, repoName, pr.GetID(), prNumber)
			if err != nil {
				return err
			}
		}
	}

	// if len(cfg.AutoMerge.Label) != 0 {
	if true {
		switch action {
		case "synchronize":
			// Remove ready-to-merge label if it is present and the developer
			// synchronized the PR
			if _, ok := c.prLabels[cfg.AutoMerge.Label]; ok {
				_, err := c.GHCli.Issues.RemoveLabelForIssue(
					context.Background(), owner, repoName, prNumber, cfg.AutoMerge.Label)
				if err != nil {
					return err
				}
				delete(c.prLabels, cfg.AutoMerge.Label)
			}
		}
		switch action {
		case "labeled", "unlabeled", "synchronize":
			cfg.AutoMerge.Label = "ready-to-merge"
			cfg.AutoMerge.MinimalApprovals = 1
			if pre.GetLabel().GetName() == cfg.AutoMerge.Label {
				return nil
			}
			if !pr.GetDraft() {
				err := c.AutoMerge(cfg.AutoMerge, owner, repoName, pr.GetBase(), pr.GetHead(), prNumber, nil)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (c *Client) HandlePRRE(cfg PRBlockerConfig, pre *gh.PullRequestReviewEvent) error {
	pr := pre.GetPullRequest()
	owner := pr.Base.Repo.GetOwner().GetLogin()
	repoName := *pr.Base.Repo.Name
	prNumber := pr.GetNumber()
	action := pre.GetAction()
	c.log.Info().Fields(map[string]interface{}{
		"action":    action,
		"pr-number": prNumber,
	}).Msg("Action triggered from PR")

	c.prLabels = ParseGHLabels(pr.Labels)

	// Unassign reviewer on approval
	if pre.Review != nil {
		if strings.ToLower(pre.Review.GetState()) == "approved" {
			users := []*gh.User{
				pre.Review.GetUser(),
			}
			err := c.Unassign(context.TODO(), owner, repoName, prNumber, users)
			if err != nil {
				c.log.Info().Fields(map[string]interface{}{
					"error":     err,
					"pr-number": prNumber,
				}).Msg("Unable to remove assignees from PR")
				return err
			}
		}
	}

	// if len(cfg.AutoMerge.Label) != 0 {
	if true {
		cfg.AutoMerge.Label = "ready-to-merge"
		cfg.AutoMerge.MinimalApprovals = 1
		if !pr.GetDraft() {
			err := c.AutoMerge(cfg.AutoMerge, owner, repoName, pr.GetBase(), pr.GetHead(), prNumber, pre.Review)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) HandleSE(cfg PRBlockerConfig, se *gh.StatusEvent) error {
	owner := se.Repo.GetOwner().GetLogin()
	repoName := *se.Repo.Name
	nextPage := 0

	if true {
		cfg.AutoMerge.Label = "ready-to-merge"
		cfg.AutoMerge.MinimalApprovals = 1
	}

	var (
		cancels               []context.CancelFunc
		urlFails              []string
		triggerRegexp         *regexp.Regexp
		err                   error
		issueKnownFlakes      map[int]jenkins.GHIssue
		jc                    *jenkins.JenkinsClient
		jobNameToJenkinsFails = map[string]jenkins.JenkinsFailures{}
	)
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	triage := cfg.FlakeTracker != nil && jenkins.IsJenkinsFailure(se.GetState(), se.GetDescription())

	if triage {
		c.Log().Info().Msg("Triaging flake")

		// Check for potential flakes
		urlFails = []string{se.GetTargetURL()}

		triggerRegexp, err = regexp.Compile(cfg.FlakeTracker.JenkinsConfig.RegexTrigger)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)

		issueKnownFlakes, err = c.GetFlakeIssues(ctx, owner, repoName, IssueCreator, cfg.FlakeTracker.IssueTracker.IssueLabels)
		if err != nil {
			return err
		}

		jc, err = jenkins.NewJenkinsClient(ctx, cfg.FlakeTracker.JenkinsConfig.JenkinsURL, false)
		if err != nil {
			return err
		}
	}

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		issues, resp, err := c.GHCli.Search.Issues(ctx, se.GetSHA(), &gh.SearchOptions{
			ListOptions: gh.ListOptions{
				Page: nextPage,
			},
		})
		if err != nil {
			return err
		}
		for _, pr := range issues.Issues {
			o, r := OwnerRepoFromRepositoryURL(pr.GetRepositoryURL())
			if o != c.orgName || r != c.repoName {
				continue
			}
			pr, _, err := c.GHCli.PullRequests.Get(ctx, o, r, pr.GetNumber())
			if err != nil {
				c.Log().Warn().Msgf("Unable to get PR for sha %s", se.GetSHA())
				continue
			}
			if pr.GetDraft() {
				c.Log().Info().Fields(map[string]interface{}{"pr-number": pr.GetNumber()}).Msgf("PR is in draft")
				continue
			}
			err = c.AutoMerge(cfg.AutoMerge, owner, repoName, pr.GetBase(), pr.GetHead(), pr.GetNumber(), nil)
			if err != nil {
				return err
			}

			if triage {
				baseBranch := pr.GetBase().GetRef()
				c.Log().Info().Fields(map[string]interface{}{
					"pr":          pr.GetNumber(),
					"base-branch": baseBranch,
				}).Msg("Triaging flake")
				err = c.TriagePRFailures(ctx, jc, cfg.FlakeTracker, pr.GetNumber(), urlFails, issueKnownFlakes, jobNameToJenkinsFails, triggerRegexp)
				if err != nil {
					return err
				}
			}
		}

		nextPage = resp.NextPage
		if nextPage != 0 {
			continue
		}
		break
	}
	return nil
}

// IsNotFound returns true if the given error is a NotFound.
func IsNotFound(err error) bool {
	return IsHTTPErrorCode(err, http.StatusNotFound)
}

func IsHTTPErrorCode(err error, httpCode int) bool {
	if err == nil {
		return false
	}

	if err, ok := err.(*gh.ErrorResponse); ok && err.Response.StatusCode == httpCode {
		return true
	}

	return false
}

func OwnerRepoFromRepositoryURL(s string) (owner, repo string) {
	path := strings.Split(s, "/")
	if len(path) < 2 {
		return "", ""
	}
	owner = path[len(path)-2]
	repo = path[len(path)-1]
	return
}
