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

	gh "github.com/google/go-github/v50/github"
)

type PRLabels map[string]struct{}

// parseGHLabels parses the github labels into a map of labels (a set)
func parseGHLabels(ghLabels []*gh.Label) PRLabels {
	lbls := make(map[string]struct{}, len(ghLabels))
	for _, prLabel := range ghLabels {
		lbls[prLabel.GetName()] = struct{}{}
	}
	return lbls
}

// subslice returns true if all elements of 's1' are keys of 's2'.
func subslice(s1 []string, s2 PRLabels) bool {
	if len(s1) > len(s2) {
		return false
	}
	for _, e := range s1 {
		if _, ok := s2[e]; !ok {
			return false
		}
	}
	return true
}

// AutoLabel sets the labels automatically in a PR that is opened or reopened.
func (c *Client) AutoLabel(labels []string, owner string, repoName string, prNumber int, prLabels PRLabels) error {
	if subslice(labels, prLabels) {
		return nil
	}

	_, _, err := c.GHCli.Issues.AddLabelsToIssue(
		context.Background(), owner, repoName, prNumber, labels)
	return err
}
