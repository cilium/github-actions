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
	"log"
	"os"

	"github.com/go-yaml/yaml"
	gh "github.com/google/go-github/v28/github"

	labeler "github.com/cilium/github-actions/pkg"
	"github.com/cilium/github-actions/pkg/github"
)

func main() {
	ghEventName := os.Getenv("GITHUB_EVENT_NAME")
	ghEventPath := os.Getenv("GITHUB_EVENT_PATH")
	ghToken := os.Getenv("GITHUB_TOKEN")
	ghSha := os.Getenv("GITHUB_SHA")
	actionCfgPath := os.Getenv("CONFIG_PATH")

	event, err := github.GetEvent(ghEventName, ghEventPath)
	if err != nil {
		log.Fatalf("Unable to get GH event: %s\n", err)
	}
	switch event := event.(type) {
	case *gh.PullRequestEvent:
		ghClient := github.NewClient(ghToken)
		owner := event.PullRequest.Base.Repo.GetOwner().GetLogin()
		repoName := event.PullRequest.Base.Repo.GetName()
		cfgFile, err := github.GetFile(ghClient, owner, repoName, actionCfgPath, ghSha)
		if err != nil {
			log.Fatalf("Unable to get config %q file: %s\n", actionCfgPath, err)
		}
		var c labeler.PRBlockerConfig
		err = yaml.Unmarshal(cfgFile, &c)
		if err != nil {
			log.Fatalf("Unable to unmarshal config %q file: %s\n", actionCfgPath, err)
		}
		l := labeler.NewClient(ghClient)
		err = l.HandlePRE(c, event)
		if err != nil {
			log.Fatalf("Unable to handle PullRequestEvent: %s\n", err)
		}
	}
}
