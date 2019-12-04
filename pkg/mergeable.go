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
	"fmt"
	"regexp"
	"time"

	gh "github.com/google/go-github/v28/github"
)

type PRLabelConfig struct {
	// RegexLabel contains the regex that will be used to find for labels.
	RegexLabel string `yaml:"regex-label,omitempty"`
	// Helper will print the a helper message in case the regex-label is or
	// isn't matched.
	Helper string `yaml:"helper,omitempty"`
	// SetLabels will set the labels in case the RegexLabel matches the labels
	// of a PR.
	SetLabels []string `yaml:"set-labels,omitempty"`
}

type BlockPRWith struct {
	// LabelsUnset blocks the PR if any of the Labels are not set, i.e., if any
	// of the regex does not match any label set in the PR.
	LabelsUnset []PRLabelConfig `yaml:"labels-unset,omitempty"`
	// LabelsSet blocks the PR if any of the Labels are set, i.e., if any of the
	// regex matches any label set in the PR.
	LabelsSet []PRLabelConfig `yaml:"labels-set,omitempty"`
}

// BlockPRWith returns true if the PR needs to be blocked based on the logic
// stored under config.BlockPRWith.
func (c *Client) BlockPRWith(blockPRConfig BlockPRWith, owner, repoName string, prNumber int, prLbls []string) (bool, []string, error) {
	var (
		blockPR      bool
		cancels      []context.CancelFunc
		blockReasons []string
	)
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	// Check which labels are not set in the PR.
	for _, lblsUnset := range blockPRConfig.LabelsUnset {
		var found bool
		for _, prLbl := range prLbls {
			matched, err := regexp.MatchString(lblsUnset.RegexLabel, prLbl)
			if err != nil {
				return false, nil, err
			}
			if matched {
				found = true
				break
			}
		}
		if found {
			// If the labels are set then remove all previously set labels that
			// are blocking the mergeability of this PR.
			for _, lbl := range lblsUnset.SetLabels {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				cancels = append(cancels, cancel)
				_, err := c.gh.Issues.RemoveLabelForIssue(ctx, owner, repoName, prNumber, lbl)
				if err != nil && !IsNotFound(err) {
					return false, nil, err
				}
			}
		} else {
			blockPR = true
			// If they are not leave helper message and add labels to help
			// users avoiding PR from being merged.
			// Don't re-print helper messages if we already have setup the
			// labels in the past.
			if lblsUnset.Helper != "" && !subslice(lblsUnset.SetLabels, prLbls) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				cancels = append(cancels, cancel)
				_, _, err := c.gh.Issues.CreateComment(ctx, owner, repoName, prNumber, &gh.IssueComment{
					Body: &lblsUnset.Helper,
				})
				if err != nil {
					return false, nil, err
				}
			}
			if len(lblsUnset.SetLabels) != 0 {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				cancels = append(cancels, cancel)
				_, _, err := c.gh.Issues.AddLabelsToIssue(ctx, owner, repoName, prNumber, lblsUnset.SetLabels)
				if err != nil {
					return false, nil, err
				}
			}
		}
	}

	// Re-fetch the PR labels since we previously had re-add and delete some
	// labels.
	prLbls, err := c.GetCurrentLabels(owner, repoName, prNumber)
	if err != nil {
		return false, nil, err
	}
	// Set the PR to be blocked if any of the labels provided by the regex is
	// currently set in the PR
	for _, lblsSet := range blockPRConfig.LabelsSet {
		for _, prLbl := range prLbls {
			matched, err := regexp.MatchString(lblsSet.RegexLabel, prLbl)
			if err != nil {
				return false, nil, err
			}
			if matched {
				blockPR = true
				if lblsSet.Helper != "" {
					blockReasons = append(blockReasons, lblsSet.Helper)
				}
				break
			}
		}
	}
	return blockPR, blockReasons, nil
}

// UpdateMergeabilityCheck sets the mergeability checker with "Success" or
// "Failure" in case the PR needs to be blocked from mergeability.
func (c *Client) UpdateMergeabilityCheck(
	owner string,
	repoName string,
	prNumber int,
	head *gh.PullRequestBranch,
	blockPR bool,
	blockReasons []string,
) error {

	const checkerName = "Mergeability"

	var (
		conclusion string
		title      string
		summary    string
		cancels    []context.CancelFunc
	)
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	if blockPR {
		conclusion = "failure"
		title = "Not mergeable!"
		summary = fmt.Sprintf("Blocking PR since it's not in a mergeable state due %s", blockReasons)
	} else {
		conclusion = "success"
		title = "Mergeable!"
		summary = "Everything is set up correctly!"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cancels = append(cancels, cancel)
	lc, _, err := c.gh.Checks.ListCheckRunsForRef(ctx, owner, repoName, head.GetRef(), &gh.ListCheckRunsOptions{
		CheckName: func() *string { a := checkerName; return &a }(),
	})
	switch {
	case err != nil && !IsNotFound(err):
		return err
	case err == nil:
		for _, cr := range lc.CheckRuns {
			for _, pr := range cr.PullRequests {
				if pr.GetNumber() == prNumber {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					cancels = append(cancels, cancel)
					_, _, err := c.gh.Checks.UpdateCheckRun(ctx, owner, repoName, cr.GetID(), gh.UpdateCheckRunOptions{
						Name:       checkerName,
						HeadBranch: head.Ref,
						HeadSHA:    head.SHA,
						Status:     func() *string { a := "completed"; return &a }(),
						Conclusion: &conclusion,
						CompletedAt: &gh.Timestamp{
							Time: time.Now(),
						},
						Output: &gh.CheckRunOutput{
							Title:   &title,
							Summary: &summary,
						},
					})
					return err
				}
			}
		}
		fallthrough
	case IsNotFound(err):
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		_, _, err := c.gh.Checks.CreateCheckRun(ctx, owner, repoName, gh.CreateCheckRunOptions{
			Name:       checkerName,
			HeadBranch: head.GetRef(),
			HeadSHA:    head.GetSHA(),
			Status:     func() *string { a := "completed"; return &a }(),
			Conclusion: &conclusion,
			CompletedAt: &gh.Timestamp{
				Time: time.Now(),
			},
			Output: &gh.CheckRunOutput{
				Title:   &title,
				Summary: &summary,
			},
			Actions: nil,
		})
		return err
	}
	return nil
}
