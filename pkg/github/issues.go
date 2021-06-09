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
	"fmt"
	"regexp"
	"strconv"

	"github.com/cilium/github-actions/pkg/jenkins"
	"github.com/cilium/github-actions/pkg/progress"
	gh "github.com/google/go-github/v35/github"
)

const (
	IssueCreator = "maintainer-s-little-helper"
)

func getDuplicate(orgName, repo, body string) int {
	dupRegex := fmt.Sprintf(`([Dd]uplicate of)[ ]+((http[s]?://(www.)?github.com/%s/%s/issue/)|(#))`, orgName, repo)
	dupRegexp := regexp.MustCompile(dupRegex + "([0-9]+)")
	issueNumberRegexp := regexp.MustCompile(dupRegex)
	if !dupRegexp.MatchString(body) {
		return 0
	}
	ghIssueNumber := issueNumberRegexp.ReplaceAllString(body, "")
	ghIssue, _ := strconv.Atoi(ghIssueNumber)
	return ghIssue
}

// GetFlakeIssues gets all "open" issues that are consider Jenkins flakes. and
// all "closed" and "open" issues created by the "creator".
func (c *Client) GetFlakeIssues(ctx context.Context, owner, repo, creator string, labels []string) (map[int]jenkins.GHIssue, error) {
	issueFailures := map[int]jenkins.GHIssue{}

	// Get All Issues
	nextPage := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		ghIssues, resp, err := c.GHCli.Issues.ListByRepo(ctx, owner, repo, &gh.IssueListByRepoOptions{
			State:  "open",
			Labels: labels,
			ListOptions: gh.ListOptions{
				Page: nextPage,
			},
		})
		if err != nil {
			return nil, err
		}
		nextPage = resp.NextPage

		if c.clientMode {
			// divide the loading bar by 2 because we have 2 for loops
			progress.PrintLoadBar(float64(resp.NextPage) / float64(resp.LastPage) / 2)
		}
		// Get open GH issues that are flakes.
		for _, issue := range ghIssues {
			ghIssue := jenkins.ParseGHIssue(issue.GetTitle(), issue.GetBody())
			issueFailures[issue.GetNumber()] = ghIssue
		}

		if nextPage == 0 {
			break
		}
	}

	// Get All Issues from the 'creator'
	nextPage = 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Get all GH issues created by MLH since it's easier to parse them.
		ghIssues, resp, err := c.GHCli.Issues.ListByRepo(ctx, owner, repo, &gh.IssueListByRepoOptions{
			State:   "all",
			Creator: creator,
			Labels:  labels,
			ListOptions: gh.ListOptions{
				Page: nextPage,
			},
		})
		if err != nil {
			return nil, err
		}
		nextPage = resp.NextPage

		if c.clientMode {
			// multiply the loading bar by 2 because we have 2 for loops
			progress.PrintLoadBar(float64(resp.NextPage) / float64(resp.LastPage) * 2)
		}

		for _, issue := range ghIssues {
			ghIssueNumber := issue.GetNumber()
			_, ok := issueFailures[ghIssueNumber]
			if ok {
				continue
			}
			// If the issue was closed by someone it will mean there's a
			// duplicate. We need to find it.
			if !issue.GetClosedAt().IsZero() {
				dupIssueNumber, err := c.findDupIssue(ctx, owner, repo, ghIssueNumber)
				if err != nil {
					return nil, err
				}
				if dupIssueNumber != 0 {
					ghIssueNumber = dupIssueNumber
				}
			}
			// We will still parse MLH GH issue but, in case it was considered
			// a duplicate, we will reference the number of the original GH
			// issue.
			ghIssue := jenkins.ParseGHIssue(issue.GetTitle(), issue.GetBody())
			issueFailures[ghIssueNumber] = ghIssue
		}

		if nextPage == 0 {
			break
		}
	}
	return issueFailures, nil
}

// findDupIssue finds the 'Duplicate Of' GH issue for in the comments of the given
// 'ghIssueNumber'.
func (c *Client) findDupIssue(ctx context.Context, owner string, repo string, ghIssueNumber int) (int, error) {
	nextPage := 0
	var dupIssueNumber int
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		ghIssues, resp, err := c.GHCli.Issues.ListComments(ctx, owner, repo, ghIssueNumber, &gh.IssueListCommentsOptions{
			ListOptions: gh.ListOptions{
				Page: nextPage,
			},
		})
		if err != nil {
			return 0, err
		}
		nextPage = resp.NextPage
		for _, ghIssue := range ghIssues {
			// If multiple people add "Duplicate of #" we will
			// consider the last comment that considers it as a
			// duplicate.
			dupNumber := getDuplicate(c.orgName, c.repoName, ghIssue.GetBody())
			if dupNumber != 0 {
				dupIssueNumber = dupNumber
			}
		}
		if nextPage == 0 {
			break
		}
	}
	return dupIssueNumber, nil
}
