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
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v30/github"
)

type ErrProjectNotFound struct {
	projectName string
}

func (e ErrProjectNotFound) Error() string {
	return fmt.Sprintf("project %q not found", e.projectName)
}

func (e *ErrProjectNotFound) Is(target error) bool {
	t, ok := target.(*ErrProjectNotFound)
	if !ok {
		return false
	}
	return e.projectName == t.projectName
}

type Project struct {
	ProjectName string `yaml:"project,omitempty"`
	ColumnName  string `yaml:"column,omitempty"`
}

// getProjectID retrieves the project ID for the given project URL.
func (c *Client) getProjectID(owner, repoName, projURL string) (int64, error) {
	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	var page int
	for {
		plo := &gh.ProjectListOptions{
			ListOptions: gh.ListOptions{
				Page:    page,
				PerPage: 10,
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		projects, resp, err := c.gh.Repositories.ListProjects(ctx, owner, repoName, plo)
		if err != nil {
			return 0, err
		}
		for _, project := range projects {
			if project.GetHTMLURL() == projURL {
				return project.GetID(), nil
			}
		}
		page = resp.NextPage
		if page == 0 {
			break
		}
	}
	return 0, nil
}

// GetColumnID returns the ProjectID and ColumnID for the given Project.
func (c *Client) GetColumnID(owner, repoName string, project Project) (int64, int64, error) {
	projectID, err := c.getProjectID(owner, repoName, project.ProjectName)
	if err != nil {
		return 0, 0, err
	}
	if projectID == 0 {
		return 0, 0, &ErrProjectNotFound{projectName: project.ProjectName}
	}

	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	var page int
	for {
		lo := &gh.ListOptions{
			Page:    page,
			PerPage: 10,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		columns, resp, err := c.gh.Projects.ListProjectColumns(ctx, projectID, lo)
		if err != nil {
			return 0, 0, err
		}
		for _, column := range columns {
			if column.GetName() == project.ColumnName {
				return projectID, column.GetID(), nil
			}
		}
		page = resp.NextPage
		if page == 0 {
			break
		}
	}
	return projectID, 0, nil
}

// PutPRInProject puts the given prID (not PR number) into the given project.
func (c *Client) PutPRInProject(owner, repoName string, prID int64, project Project) error {
	_, columnID, err := c.GetColumnID(owner, repoName, project)
	if err != nil {
		return err
	}
	if columnID == 0 {
		return fmt.Errorf("column %q not found in project %q", project.ColumnName, project.ProjectName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, err = c.gh.Projects.CreateProjectCard(ctx, columnID, &gh.ProjectCardOptions{
		ContentID:   prID,
		ContentType: "PullRequest",
	})
	return err
}

// getIDFromURL returns the ID from the GitHub URL.
func getIDFromURL(url string) int {
	projURLSlices := strings.Split(url, "/")
	if len(projURLSlices) != 0 {
		i, _ := strconv.ParseInt(projURLSlices[len(projURLSlices)-1], 10, 32)
		return int(i)
	}
	return -1
}

// FindPRInProject finds the given PR number in the project. Returns the column
// and card ID for the given PR number. If not found, returns 0.
func (c *Client) FindPRInProject(owner, repoName string, prNumber int, project Project) (int64, int64, error) {
	_, columnID, err := c.GetColumnID(owner, repoName, project)
	if err != nil {
		return 0, 0, err
	}
	if columnID == 0 {
		return 0, 0, nil
	}

	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	var page int
	for {
		pclo := &gh.ProjectCardListOptions{
			ListOptions: gh.ListOptions{
				Page:    page,
				PerPage: 10,
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		projectCards, resp, err := c.gh.Projects.ListProjectCards(ctx, columnID, pclo)
		if err != nil {
			return 0, 0, err
		}
		for _, projectCard := range projectCards {
			if contentID := getIDFromURL(projectCard.GetContentURL()); contentID == prNumber {
				return columnID, projectCard.GetID(), nil
			}
		}
		page = resp.NextPage
		if page == 0 {
			break
		}
	}
	return 0, 0, nil
}

// GetOrCreateColumnID returns the column ID for the given project. If the
// column is not found in the project then it will be automatically created.
func (c *Client) GetOrCreateColumnID(owner, repoName string, proj Project) (int64, error) {
	projectID, columnID, err := c.GetColumnID(owner, repoName, proj)
	if err != nil {
		return 0, err
	}
	if columnID != 0 {
		return columnID, nil
	}

	c.log.Debug().Fields(map[string]interface{}{
		"project-name": proj.ProjectName,
		"column-name":  proj.ColumnName,
	}).Msg("Column not found in project")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	column, _, err := c.gh.Projects.CreateProjectColumn(ctx, projectID, &gh.ProjectColumnOptions{
		Name: proj.ColumnName,
	})
	if err != nil {
		return 0, err
	}
	return column.GetID(), nil
}

// SyncPRProjects syncs the given prID (not PR Number) across all projects
// defined in the moveToProjectsForLabelsXORed
func (c *Client) SyncPRProjects(
	moveToProjectsForLabelsXORed map[string]map[string]Project,
	owner,
	repoName string,
	prID int64,
	prNumber int,
) error {

	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	for _, branchCfgs := range moveToProjectsForLabelsXORed {
		var (
			addToProj    Project
			delFromProjs []Project
			lblsSet      []string
		)
		// Since the labels are suppose to be XORed we will remove the PR
		// from all other projects-columns that are not selected by the label.
		for label, branchCfg := range branchCfgs {
			var set bool
			for currentLabel := range c.prLabels {
				if label == currentLabel {
					addToProj = branchCfg
					set = true
					lblsSet = append(lblsSet, label)
					break
				}
			}
			if !set {
				delFromProjs = append(delFromProjs, branchCfg)
			}
		}
		if len(lblsSet) > 1 {
			c.log.Info().Fields(map[string]interface{}{
				"labels-set": lblsSet,
			}).Msg("Multiple labels set not applying any action for projects. Pick one of set")
			continue
		}
		emptyProject := Project{}
		if addToProj != emptyProject {
			var cardColumnID, cardID int64
			// Remove the PR from all other projects
			for _, delFromProj := range delFromProjs {
				var err error
				cardColumnID, cardID, err = c.FindPRInProject(owner, repoName, prNumber, delFromProj)
				if err != nil {
					// Ignore the error if the project was not found. It might mean
					// the project was closed so we don't need to track this PR on
					// it.
					if errors.Is(err, &ErrProjectNotFound{projectName: delFromProj.ProjectName}) {
						continue
					}
					return err
				}
				if cardID != 0 {
					break
				}
			}
			// And put the PR in the project associated with the only label
			// set in the PR.
			columnID, err := c.GetOrCreateColumnID(owner, repoName, addToProj)
			if err != nil {
				// Ignore the error if the project was not found. It might mean
				// the project was closed so we don't need to track this PR on
				// it.
				if errors.Is(err, &ErrProjectNotFound{projectName: addToProj.ProjectName}) {
					continue
				}
				return err
			}
			if cardID != 0 {
				if columnID != cardColumnID {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					cancels = append(cancels, cancel)
					_, err = c.gh.Projects.MoveProjectCard(ctx, cardID, &gh.ProjectCardMoveOptions{
						Position: "top",
						ColumnID: columnID,
					})
					if err != nil {
						return err
					}
				}
			} else {
				// card was not found so we have to create it!
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				cancels = append(cancels, cancel)
				_, _, err := c.gh.Projects.CreateProjectCard(ctx, columnID, &gh.ProjectCardOptions{
					ContentID:   prID,
					ContentType: "PullRequest",
				})
				if err != nil && !IsHTTPErrorCode(err, http.StatusUnprocessableEntity) {
					return err
				}
			}
		} else {
			// If no label was found we need to be sure that we will remove
			// the PR from all projects associated for the given
			// MoveToProjectsForLabelsXORed configuration.
			for _, delFromProj := range delFromProjs {
				_, cardID, err := c.FindPRInProject(owner, repoName, prNumber, delFromProj)
				if err != nil {
					// Ignore the error if the project was not found. It might mean
					// the project was closed so we don't need to track this PR on
					// it.
					if errors.Is(err, &ErrProjectNotFound{projectName: delFromProj.ProjectName}) {
						continue
					}
					return err
				}
				if cardID == 0 {
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				cancels = append(cancels, cancel)
				_, err = c.gh.Projects.DeleteProjectCard(ctx, cardID)
				if err != nil && !IsNotFound(err) {
					return err
				}
			}
		}
	}
	return nil
}
