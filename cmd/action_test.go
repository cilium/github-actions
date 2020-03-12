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

package main

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"

	"github.com/cilium/github-actions/pkg"
)

func TestConfigParser(t *testing.T) {

	expect := actions.PRBlockerConfig{
		Project: actions.Project{
			ProjectName: "https://github.com/cilium/cilium/projects/80",
			ColumnName:  "In progress",
		},
		MoveToProjectsForLabelsXORed: map[string]map[string]actions.Project{
			"v1.6": {
				"needs-backport/1.6": {
					ProjectName: "https://github.com/cilium/cilium/projects/91",
					ColumnName:  "Needs backport from master",
				},
				"backport-pending/1.6": {
					ProjectName: "https://github.com/cilium/cilium/projects/91",
					ColumnName:  "Backport pending to v1.6",
				},
				"backport-done/1.6": {
					ProjectName: "https://github.com/cilium/cilium/projects/91",
					ColumnName:  "Backport done to v1.6",
				},
			},
			"v1.5": {
				"needs-backport/1.5": {
					ProjectName: "https://github.com/cilium/cilium/projects/92",
					ColumnName:  "Needs backport from master",
				},
				"backport-pending/1.5": {
					ProjectName: "https://github.com/cilium/cilium/projects/92",
					ColumnName:  "Backport pending to v1.5",
				},
				"backport-done/1.5": {
					ProjectName: "https://github.com/cilium/cilium/projects/92",
					ColumnName:  "Backport done to v1.5",
				},
			},
		},
		RequireMsgsInCommit: []actions.MsgInCommit{
			{
				Msg:    "Signed-off-by",
				Helper: "https://docs.cilium.io/en/stable/contributing/contributing/#developer-s-certificate-of-origin",
				SetLabels: []string{
					"dont-merge/needs-sign-off",
				},
			},
		},
		AutoLabel: []string{
			"pending-review",
		},
		BlockPRWith: actions.BlockPRWith{
			LabelsUnset: []actions.PRLabelConfig{
				{
					RegexLabel: "release-note/.*",
					Helper:     "Please set the appropriate release note label.",
					SetLabels: []string{
						"dont-merge/needs-release-note",
					},
				},
			},
			LabelsSet: []actions.PRLabelConfig{
				{
					RegexLabel: "dont-merge/.*",
					Helper:     "Blocking mergeability of PR as 'dont-merge/.*' labels are set",
				},
			},
		},
	}

	file, err := os.Open("../test/config.yml")
	if err != nil {
		t.Fatal(err)
	}

	contents, err := ioutil.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	var c actions.PRBlockerConfig
	err = yaml.Unmarshal(contents, &c)
	assert.Equal(t, expect, c)
}
