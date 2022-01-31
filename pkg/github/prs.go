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
	"regexp"

	"github.com/cilium/github-actions/pkg/progress"
	gh "github.com/google/go-github/v40/github"
)

// GetPRsFailures returns a map of 'open' non-draft PRs Numbers that maps to
// jenkins failures URLs.
func (c *Client) GetPRsFailures(ctx context.Context, base string) (map[int][]string, error) {
	prFailures := map[int][]string{}
	nextPage := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		prs, resp, err := c.GHCli.PullRequests.List(ctx, c.orgName, c.repoName, &gh.PullRequestListOptions{
			State: "open",
			Base:  base,
			ListOptions: gh.ListOptions{
				Page: nextPage,
			},
		})
		if err != nil {
			panic(err)
		}
		nextPage = resp.NextPage
		if c.clientMode {
			progress.PrintLoadBar(float64(resp.NextPage) / float64(resp.LastPage))
		}
		for _, pr := range prs {
			ciFailures, err := c.GetPRFailure(ctx, pr)
			if err != nil {
				return nil, err
			}
			if len(ciFailures) == 0 {
				continue
			}
			prFailures[pr.GetNumber()] = ciFailures
		}

		if nextPage == 0 {
			break
		}
	}
	return prFailures, nil
}

func (c *Client) GetPRFailure(ctx context.Context, pr *gh.PullRequest) ([]string, error) {
	// Exclude drafts
	if pr.GetDraft() {
		includeDraft, ok := ctx.Value("include-draft").(bool)
		if !includeDraft || !ok {
			return nil, nil
		}
	}
	prSHA := pr.GetHead().GetSHA()

	return c.GetFailedJenkinsURLs(ctx, c.orgName, c.repoName, prSHA)
}

// GetPRFailures gets the jenkins URL failures of the given PR number.
func (c *Client) GetPRFailures(ctx context.Context, prNumber int) ([]string, error) {
	pr, _, err := c.GHCli.PullRequests.Get(ctx, c.orgName, c.repoName, prNumber)
	if err != nil {
		return nil, err
	}
	return c.GetPRFailure(ctx, pr)
}

// getPRTriggerComment returns the last comment that matches the given regex.
func (c *Client) getPRTriggerComment(ctx context.Context, orgName, repo string, prNumber int, regex *regexp.Regexp) (*gh.IssueComment, error) {
	var triggeredComment *gh.IssueComment
	nextPage := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		comments, resp, err := c.GHCli.Issues.ListComments(ctx, orgName, repo, prNumber, &gh.IssueListCommentsOptions{
			ListOptions: gh.ListOptions{
				Page: nextPage,
			},
		})
		if err != nil {
			panic(err)
		}

		nextPage = resp.NextPage

		if c.clientMode {
			progress.PrintLoadBar(float64(resp.NextPage) / float64(resp.LastPage))
		}

		for _, comment := range comments {
			if regex.MatchString(comment.GetBody()) {
				// We only want the last comment with the trigger phrase.
				triggeredComment = comment
			}
		}

		if nextPage == 0 {
			break
		}
	}
	return triggeredComment, nil
}

func (c *Client) createComment(ctx context.Context, orgName, repo string, number int, body string) error {
	_, _, err := c.GHCli.Issues.CreateComment(ctx, orgName, repo, number, &gh.IssueComment{
		Body: &body,
	})
	return err
}

func (c *Client) editComment(ctx context.Context, orgName, repo string, commentID int64, body string) error {
	_, _, err := c.GHCli.Issues.EditComment(ctx, orgName, repo, commentID, &gh.IssueComment{
		Body: &body,
	})
	return err
}

// CreateOrAppendComment creates or appends the 'comment' into the last comment
// from the PR 'prNumber' that matches the regex 'triggerRegexp'. If
// 'triggerRegexp' is not found then a new comment is created in the PR
// 'prNumber'.
func (c *Client) CreateOrAppendComment(ctx context.Context, prNumber int, comment string, triggerRegexp *regexp.Regexp) error {
	orgName := c.orgName
	repoName := c.repoName
	prComment, err := c.getPRTriggerComment(ctx, orgName, repoName, prNumber, triggerRegexp)
	if err != nil {
		return err
	}

	return c.CreateOrAppendCommentIssueComment(ctx, prNumber, prComment, comment)
}

// CreateOrAppendCommentIssueComment appends the 'comment' into 'issueComment'.
// If 'issueComment' is nil, it will create a new comment in the PR with the
// 'prNumber'.
func (c *Client) CreateOrAppendCommentIssueComment(ctx context.Context, prNumber int, issueComment *gh.IssueComment, comment string) error {
	orgName := c.orgName
	repoName := c.repoName

	if issueComment == nil {
		err := c.createComment(ctx, orgName, repoName, prNumber, comment)
		if err != nil {
			return err
		}
	} else {
		body := issueComment.GetBody() + "\n\n" + comment
		err := c.editComment(ctx, orgName, repoName, issueComment.GetID(), body)
		if err != nil {
			return err
		}
	}
	return nil
}
