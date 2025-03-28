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

package github

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

func TestConfigParser(t *testing.T) {

	expect := PRBlockerConfig{
		RequireMsgsInCommit: []MsgInCommit{
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
		BlockPRWith: BlockPRWith{
			LabelsUnset: []PRLabelConfig{
				{
					RegexLabel: "release-note/.*",
					Helper:     "Release note label not set, please set the appropriate release note.",
					SetLabels: []string{
						"dont-merge/needs-release-note",
					},
				},
			},
			LabelsSet: []PRLabelConfig{
				{
					RegexLabel: "dont-merge/.*",
					Helper:     "Blocking mergeability of PR as 'dont-merge/.*' labels are set",
				},
			},
		},
	}

	file, err := os.Open("testdata/config.yml")
	if err != nil {
		t.Fatal(err)
	}

	contents, err := ioutil.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	var c PRBlockerConfig
	err = yaml.Unmarshal(contents, &c)
	assert.Equal(t, expect, c)
}
