// Copyright 2019 Authors of Cilium
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

package actions

import (
	"log"
	"net/http"

	gh "github.com/google/go-github/v28/github"
)

type PRBlockerConfig struct {
	Project                      `yaml:",inline"`
	MoveToProjectsForLabelsXORed map[string]map[string]Project `yaml:"move-to-projects-for-labels-xored,omitempty"`
	RequireMsgsInCommit          []MsgInCommit                 `yaml:"require-msgs-in-commit,omitempty"`
	AutoLabel                    []string                      `yaml:"auto-label,omitempty"`
	BlockPRWith                  BlockPRWith                   `yaml:"block-pr-with,omitempty"`
}

type Client struct {
	gh *gh.Client
}

func NewClient(ghClient *gh.Client) *Client {
	return &Client{
		gh: ghClient,
	}
}

func (c *Client) HandlePRE(cfg PRBlockerConfig, pre *gh.PullRequestEvent) error {
	pr := pre.GetPullRequest()
	owner := pr.Base.Repo.GetOwner().GetLogin()
	repoName := *pr.Base.Repo.Name
	prNumber := pr.GetNumber()
	action := pre.GetAction()
	log.Printf("Action triggered %s\n", action)

	// Autolabel PRs as soon they are created
	if len(cfg.AutoLabel) != 0 { // We only auto-label PRs if when they are open / reopen
		if action == "opened" || action == "reopened" {
			prLbls := ParseGHLabels(pr.Labels)
			err := c.AutoLabel(cfg.AutoLabel, owner, repoName, prNumber, prLbls)
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
			case "labeled", "unlabeled", "synchronize":
				prLbls := ParseGHLabels(pr.Labels)
				blockPR, blockReasons, err := c.BlockPRWith(cfg.BlockPRWith, owner, repoName, prNumber, prLbls)
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
				return err
			}
		}
	}

	// Put PR in projects for backport release tracking
	if len(cfg.MoveToProjectsForLabelsXORed) != 0 {
		switch action {
		case "labeled", "unlabeled":
			prLbls := ParseGHLabels(pr.Labels)
			err := c.SyncPRProjects(cfg.MoveToProjectsForLabelsXORed, owner, repoName, pr.GetID(), prNumber, prLbls)
			if err != nil {
				return err
			}
		}
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
