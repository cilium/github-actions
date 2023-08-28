// Copyright 2021 Authors of Cilium
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
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"regexp"

	"github.com/cilium/github-actions/pkg/github"
	"github.com/cilium/github-actions/pkg/jenkins"
	struct_loader "github.com/cilium/github-actions/pkg/struct-loader"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
)

var (
	orgName    string
	repoName   string
	prNumber   int
	clientMode bool
	config     string
	baseBranch string
)

func init() {
	flag.StringVar(&orgName, "org", "cilium", "GitHub organization name (for client-mode)")
	flag.StringVar(&repoName, "repo", "cilium", "GitHub organization name (for client-mode)")
	flag.IntVar(&prNumber, "pr", 0, "PR to check for flakes (for client-mode)")
	flag.StringVar(&baseBranch, "branch", "main", "Base branch name (for client-mode)")
	flag.StringVar(&config, "config", "", "Flake config file (for client-mode)")
	flag.BoolVar(&clientMode, "client-mode", false, "Runs MLH in client mode (useful for development)")
	flag.Parse()

	go signals()
}

func signals() {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)
	<-signalCh
	cancel()
}

var (
	globalCtx, cancel = context.WithCancel(context.Background())
)

func runClient() {
	cfg, err := loadConfig(config)
	if err != nil {
		panic(err)
	}

	triggerRegexp := regexp.MustCompile(cfg.FlakeTracker.JenkinsConfig.RegexTrigger)

	useCache := false

	if len(cfg.FlakeTracker.JenkinsConfig.StableJobNames) != len(cfg.FlakeTracker.JenkinsConfig.PRJobNames) {
		panic(fmt.Sprintf("%s jobs and PR jobs should have the same length", baseBranch))
	}
	var (
		prToFailJenkinsURLs   = map[int][]string{}
		jobNameToJenkinsFails map[string]jenkins.JenkinsFailures
		issueKnownFlakes      map[int]jenkins.GHIssue
		ghClient              *github.Client
	)
	if useCache {
		err := struct_loader.LoadStruct("./gh-pr-jenkins-urls.json", &prToFailJenkinsURLs)
		if err != nil {
			panic(err)
		}

		err = struct_loader.LoadStruct("./gh-issues.json", &issueKnownFlakes)
		if err != nil {
			panic(err)
		}

		err = struct_loader.LoadStruct(fmt.Sprintf("./%s-cache.json", baseBranch), &jobNameToJenkinsFails)
		if err != nil {
			panic(err)
		}
	} else {
		fmt.Printf("Getting PRs from GH\n")
		ghClient = github.NewClient(os.Getenv("GITHUB_TOKEN"), orgName, repoName, zerolog.Ctx(globalCtx))

		if prNumber != 0 {
			err := ghClient.CommitContains(cfg.RequireMsgsInCommit, orgName, repoName, prNumber)
			if err != nil {
				panic(err)
			}

			ctx := context.WithValue(globalCtx, "include-draft", true)
			prJenkinsURLFail, err := ghClient.GetPRFailures(ctx, prNumber)
			if err != nil {
				panic(err)
			}
			if len(prJenkinsURLFail) != 0 {
				prToFailJenkinsURLs[prNumber] = prJenkinsURLFail
			}
		} else {
			prToFailJenkinsURLs, err = ghClient.GetPRsFailures(globalCtx, baseBranch)
			if err != nil {
				panic(err)
			}
		}

		if len(prToFailJenkinsURLs) == 0 {
			fmt.Println("No failures found in PRs!")
			return
		}

		err = struct_loader.StoreStruct("./gh-pr-jenkins-urls.json", prToFailJenkinsURLs)
		if err != nil {
			panic(err)
		}

		// GH Issues
		fmt.Printf("Getting Issues from GH\n")

		issueKnownFlakes, err = ghClient.GetFlakeIssues(globalCtx, orgName, repoName, github.IssueCreator, cfg.FlakeTracker.IssueTracker.IssueLabels)
		if err != nil {
			panic(err)
		}
		if len(issueKnownFlakes) == 0 {
			fmt.Println("No failures found in issues!")
		}

		err = struct_loader.StoreStruct("./gh-issues.json", issueKnownFlakes)
		if err != nil {
			panic(err)
		}
		jobNameToJenkinsFails = map[string]jenkins.JenkinsFailures{}
	}

	jc, err := jenkins.NewJenkinsClient(globalCtx, cfg.FlakeTracker.JenkinsConfig.JenkinsURL, false)
	if err != nil {
		panic(err)
	}

	for prNumber, urlFails := range prToFailJenkinsURLs {
		select {
		case <-globalCtx.Done():
			return
		default:
		}

		err := ghClient.TriagePRFailures(globalCtx, jc, cfg.FlakeTracker, prNumber, urlFails, issueKnownFlakes, jobNameToJenkinsFails, triggerRegexp)
		if err != nil {
			panic(err)
		}
	}
}

func loadConfig(cfgFile string) (*github.PRBlockerConfig, error) {
	b, err := ioutil.ReadFile(cfgFile)
	if err != nil {
		return nil, err
	}

	var cfg github.PRBlockerConfig
	err = yaml.Unmarshal(b, &cfg)
	if err != nil {
		return nil, err
	}
	if cfg.FlakeTracker == nil {
		cfg.FlakeTracker = &github.FlakeConfig{}
	}

	return &cfg, nil
}
