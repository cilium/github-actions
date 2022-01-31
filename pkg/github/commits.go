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
	"strings"
	"time"

	gh "github.com/google/go-github/v40/github"
)

type MsgInCommit struct {
	// Msg is the message that should be found in the commit message.
	Msg string `yaml:"msg,omitempty"`
	// Helper is the message that should be printed if 'Msg' is not found in
	// the commit message.
	Helper string `yaml:"helper,omitempty"`
	// SetLabels are the labels to be set in the PR for which Msg was not found.
	SetLabels []string `yaml:"set-labels,omitempty"`
}

// commitsContains checks if the all commits of the given prNumber contains the
// given msg.
// Returns a slice of commit IDs that don't contain the given msg or an error
// in case of an error.
func (c *Client) commitContains(owner, repoName string, prNumber int, msg string) ([]string, error) {
	var (
		missSignOff []string
		cancels     []context.CancelFunc
		page        int
	)
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		opts := &gh.ListOptions{
			Page:    page,
			PerPage: 10,
		}
		commits, resp, err := c.GHCli.PullRequests.ListCommits(ctx, owner, repoName, prNumber, opts)
		if err != nil {
			return nil, err
		}
		for _, commit := range commits {
			if !strings.Contains(commit.GetCommit().GetMessage(), msg) {
				missSignOff = append(missSignOff, commit.GetSHA())
			}
		}
		page = resp.NextPage
		if page == 0 {
			break
		}
	}
	return missSignOff, nil
}

// CommitContains checks if all commits of the given PR Number contains the
// each msg provided for each MsgInCommit.
func (c *Client) CommitContains(msgsInCommit []MsgInCommit, owner, repoName string, prNumber int) error {
	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	for _, msgRequired := range msgsInCommit {
		commits, err := c.commitContains(owner, repoName, prNumber, msgRequired.Msg)
		if err != nil {
			return err
		}
		if len(commits) == 0 {
			for _, lbl := range msgRequired.SetLabels {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				cancels = append(cancels, cancel)
				_, err := c.GHCli.Issues.RemoveLabelForIssue(ctx, owner, repoName, prNumber, lbl)
				if err != nil && !IsNotFound(err) {
					return err
				}
			}
			continue
		}
		var comment string
		if len(commits) == 1 {
			comment = fmt.Sprintf("Commit %%s does not contain %q.", msgRequired.Msg)
		} else {
			comment = fmt.Sprintf("Commits %%s do not contain %q.", msgRequired.Msg)
		}
		if msgRequired.Helper != "" {
			comment += fmt.Sprintf("\n\nPlease follow instructions provided in %s", msgRequired.Helper)
		}
		comment = fmt.Sprintf(comment, strings.Join(commits, ", "))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		_, _, err = c.GHCli.Issues.CreateComment(ctx, owner, repoName, prNumber, &gh.IssueComment{
			Body: &comment,
		})
		if err != nil {
			return err
		}
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		_, _, err = c.GHCli.Issues.AddLabelsToIssue(ctx, owner, repoName, prNumber, msgRequired.SetLabels)
		if err != nil {
			return err
		}
	}
	return nil
}
