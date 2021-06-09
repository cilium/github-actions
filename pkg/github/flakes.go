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

package github

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cilium/github-actions/pkg/jenkins"
)

type FlakeConfig struct {
	IssueTracker     IssueTrackConfig `yaml:"issue-tracker-config"`
	JenkinsConfig    JenkinsConfig    `yaml:"jenkins-config"`
	MaxFlakesPerTest int              `yaml:"max-flakes-per-test"`
	FlakeSimilarity  float64          `yaml:"flake-similarity"`
	IgnoreFailures   []string         `yaml:"ignore-failures"`
}

// CommonFailure returns true if the given 'str' is part of the list of failures
// that should be ignored.
func (fc *FlakeConfig) CommonFailure(str string) bool {
	for _, failure := range fc.IgnoreFailures {
		if strings.Contains(str, failure) {
			return true
		}
	}
	return false
}

func (fc *FlakeConfig) GetMaxFlakesPerTest() int {
	return fc.MaxFlakesPerTest
}

type IssueTrackConfig struct {
	// Contains the labels that are going to be used to create the GH issues
	// tracked by MLH.
	IssueLabels []string `yaml:"issue-labels"`
}

type StableJobs struct {
	JobNames []string `yaml:"correlated-with-stable-jobs"`
}

type JenkinsConfig struct {
	// JenkinsURL is the URL to access jenkins
	JenkinsURL string `yaml:"jenkins-url"`

	// RegexTrigger which is used to find which comment has triggered the CI
	// and so MLH can edit the comment with the tests failures.
	RegexTrigger string `yaml:"regex-trigger"`

	// StableJobNames contains the list of job names that are "stable" i.e. the
	// ones that have scheduled runs.
	StableJobNames []string `yaml:"stable-jobs"`

	// PRJobNames maps a PR job name to a list of stable jobs that are used to
	// correlate if a test failure is a flake or not.
	PRJobNames map[string]StableJobs `yaml:"pr-jobs"`
}

func (c *Client) TriagePRFailure(
	ctx context.Context,
	jc *jenkins.JenkinsClient,
	flakeCfg *FlakeConfig,
	prNumber int,
	jobFailURL string,
	issueKnownFlakes map[int]jenkins.GHIssue,
	jobNameToJenkinsFails map[string]jenkins.JenkinsFailures,
) (
	prJobName string,
	flakesFound map[int][]string,
	flakesNotFound []jenkins.BuildFailure,
	err error,
) {

	prJobName, jobNumber := jenkins.SplitJobNameNumber(jobFailURL)

	// Get the stable job names that will be used to correlate flakes against
	stableJobs, ok := flakeCfg.JenkinsConfig.PRJobNames[prJobName]
	if !ok {
		c.Log().Warn().Fields(
			map[string]interface{}{
				"job-name":  prJobName,
				"pr-number": prNumber,
			}).Msg("Job not found")
		return "", nil, nil, nil
	}
	_, jobFailures, err := jc.GetJobFailure(ctx, prJobName, jobNumber)
	if err != nil {
		return "", nil, nil, fmt.Errorf("unable to get job failure #%d from job %q", jobNumber, prJobName)
	}

	// Ignore common failures and get the potential flakes
	prFailures := jenkins.FilterFlakes(c.Log(), flakeCfg, jobFailures, jobFailURL)
	if len(prFailures) == 0 {
		return "", nil, nil, nil
	}

	// Store the flakes found so that we can report back in a form of a PR
	// comment.
	flakesFound = map[int][]string{}

prFailList:
	for _, prFailure := range prFailures {
		// Check if there is a GH issue already created for this flake
		// FIXME filter GH issues per stable branch? Could be as easy to have
		//  a specific label for each branch.
		ghIssueNumber, similarity := jenkins.CheckGHIssuesFailures(issueKnownFlakes, prFailure.Test, flakeCfg.FlakeSimilarity)
		if ghIssueNumber != -1 {
			c.Log().Info().Fields(map[string]interface{}{
				"gh-issue-number": ghIssueNumber,
				"test-name":       prFailure.TestName,
				"pr-number":       prNumber,
			}).Msg("Found flake in GH Issue")

			// Generate GH Issue comment
			_, body, err := jenkins.GHIssueComment(prNumber, 100*similarity, prFailure)
			if err != nil {
				return "", nil, nil, fmt.Errorf("unable to comment on GH for PR #%d: %w", prNumber, err)
			}

			// Comment into the GH issue or re-open the GH issue was closed
			err = c.CommentAndOpenIssue(ctx, c.orgName, c.repoName, ghIssueNumber, body)
			if err != nil {
				return "", nil, nil, fmt.Errorf("unable to comment on the GH issue #%d: %w", ghIssueNumber, err)
			}
			flakesFound[ghIssueNumber] = append(flakesFound[ghIssueNumber], fmt.Sprintf("%.2f", 100*similarity))
			continue
		}

		// Check if this flake is being hit in the current stable branches.
		// If we find it then we can safely assume it's a flake.
		for _, jobName := range stableJobs.JobNames {

			stableJenkinsFails, ok := jobNameToJenkinsFails[jobName]
			if !ok {
				fmt.Printf("Not found in cache, retriving...")
				stableBrTestFails, err := jc.GetJobFrom(ctx, jobName, func(jobFailures []jenkins.BuildFailure, jobFailURL string) []jenkins.BuildFailure {
					return jenkins.FilterFlakes(c.Log(), flakeCfg, jobFailures, jobFailURL)
				})
				if err != nil {
					return "", nil, nil, err
				}

				jobNameToJenkinsFails[jobName] = stableBrTestFails
				stableJenkinsFails = stableBrTestFails
			}

			for _, stableJenkinsFail := range stableJenkinsFails {
				for _, stableBuildFail := range stableJenkinsFail {

					// Check if this PR failure is similar to the stable failure.
					similarity = jenkins.IsSimilarFlake(stableBuildFail.Test, prFailure.Test, flakeCfg.FlakeSimilarity)
					if similarity == -1 {
						continue
					}

					// Generate GH Issue comment
					title, body, err := jenkins.GHIssueComment(prNumber, 100*similarity, prFailure)
					if err != nil {
						panic(err)
					}

					// Create a GH issue or re-open it if the issue was closed
					issueNumber, err := c.CreateIssue(ctx, c.orgName, c.repoName, title, body, flakeCfg.IssueTracker.IssueLabels)
					if err != nil {
						return "", nil, nil, fmt.Errorf("unable to create GH issue: %w", err)
					}

					c.Log().Info().Fields(map[string]interface{}{
						"gh-issue-number": ghIssueNumber,
						"test-name":       prFailure.TestName,
						"pr-number":       prNumber,
					}).Msg("Found new flake from stable branch. Created new GH issue")

					issueKnownFlakes[issueNumber] = jenkins.GHIssue{
						// FIXME the title is not exactly this one
						Title: prFailure.TestName,
						Test:  prFailure.Test,
					}
					flakesFound[issueNumber] = append(flakesFound[issueNumber], fmt.Sprintf("%.2f", 100*similarity))
					continue prFailList
				}
			}
		}
		c.Log().Info().Fields(map[string]interface{}{
			"test-name": prFailure.TestName,
			"pr-number": prNumber,
		}).Msg("Not found flake in GH Issue")

		flakesNotFound = append(flakesNotFound, prFailure)
	}

	return prJobName, flakesFound, flakesNotFound, nil
}

func (c *Client) TriagePRFailures(
	ctx context.Context,
	jc *jenkins.JenkinsClient,
	flakeCfg *FlakeConfig,
	prNumber int,
	urlFails []string,
	issueKnownFlakes map[int]jenkins.GHIssue,
	jobNameToJenkinsFails map[string]jenkins.JenkinsFailures,
	triggerRegexp *regexp.Regexp) error {

	for _, jobFailURL := range urlFails {
		prJobName, failsFound, failsNotFound, err := c.TriagePRFailure(ctx, jc, flakeCfg, prNumber, jobFailURL, issueKnownFlakes, jobNameToJenkinsFails)
		if err != nil {
			return err
		}

		var knownFlakes string
		switch {
		case len(failsFound) == 0 && len(failsNotFound) == 0:
			continue
		case len(failsNotFound) == 0:
			// If we only have found flakes exclusively

			// TODO add link for the flake confidence
			// Comment flakes in PR
			knownFlakes, err = jenkins.PRCommentKnownFlakes(prJobName, failsFound)
		case len(failsFound) > 0:
			// In case we have found known flakes and hit new failures, those
			// new failures could potentially be new flakes.

			knownFlakes, err = jenkins.PRCommentUnknownFlakes(prJobName, failsNotFound, failsFound)
		case len(failsNotFound) == 1:
			// If it had a single failure it might be a new flake, if
			// it had more than 1 failure then it's more likely to be a real
			// failure.

			knownFlakes, err = jenkins.PRCommentFailure(failsNotFound[0])
		}
		if err != nil {
			panic(err)
		}

		if knownFlakes != "" {
			err = c.CreateOrAppendComment(ctx, prNumber, knownFlakes, triggerRegexp)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
