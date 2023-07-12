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
	"errors"
	"fmt"
	"os"
	"strings"
)

type PRBlockerConfig struct {
	Project                      `yaml:",inline"`
	MoveToProjectsForLabelsXORed map[string]map[string]Project `yaml:"move-to-projects-for-labels-xored,omitempty"`
	RequireMsgsInCommit          []MsgInCommit                 `yaml:"require-msgs-in-commit,omitempty"`
	AutoLabel                    []string                      `yaml:"auto-label,omitempty"`
	BlockPRWith                  BlockPRWith                   `yaml:"block-pr-with,omitempty"`
	AutoMerge                    AutoMerge                     `yaml:"auto-merge,omitempty"`
	FlakeTracker                 *FlakeConfig                  `yaml:"flake-tracker,omitempty"`
}

func GetActionsCfg(ghClient *Client, owner, repoName, ghSha string) (string, []byte, error) {
	actionCfgPath := os.Getenv("CONFIG_PATHS")
	configPaths := strings.Split(actionCfgPath, ",")
	for _, configPath := range configPaths {
		cfgFile, err := ghClient.GetConfigFile(owner, repoName, configPath, ghSha)
		switch {
		case IsNotFound(err) || IsNotFound(errors.Unwrap(err)):
			continue
		case err != nil:
			return "", nil, fmt.Errorf("unable to get config %q file: %s %T\n", configPath, err, errors.Unwrap(err))
		}
		return configPath, cfgFile, nil
	}
	return "", nil, nil
}
