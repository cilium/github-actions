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

	gh "github.com/google/go-github/v30/github"
)

// ParseGHLabels parses the github labels into
func ParseGHLabels(ghLabels []*gh.Label) []string {
	var lbls []string
	for _, prLabel := range ghLabels {
		lbls = append(lbls, prLabel.GetName())
	}
	return lbls
}

// contains returns true if 's' contains 'e'.
func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// subslice returns true if 's1' is a subslice of 's2'.
func subslice(s1, s2 []string) bool {
	if len(s1) > len(s2) {
		return false
	}
	for _, e := range s1 {
		if !contains(s2, e) {
			return false
		}
	}
	return true
}

// GetCurrentLabels retrieve current labels of the given prNumber.
func (c *Client) GetCurrentLabels(owner string, repoName string, prNumber int) ([]string, error) {
	opts := gh.ListOptions{}
	currLabels, _, err := c.gh.Issues.ListLabelsByIssue(
		context.Background(), owner, repoName, prNumber, &opts)

	if err != nil {
		return nil, err
	}

	return ParseGHLabels(currLabels), nil
}

// AutoLabel sets the labels automatically in a PR that is opened or reopened.
func (c *Client) AutoLabel(labels []string, owner string, repoName string, prNumber int, currentPRLabels []string) error {
	if subslice(labels, currentPRLabels) {
		return nil
	}

	_, _, err := c.gh.Issues.AddLabelsToIssue(
		context.Background(), owner, repoName, prNumber, labels)
	return err
}
