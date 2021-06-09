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

package jenkins

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// IsSimilarFlake checks if test1 and test2 have a similarity greater than
// 'flakeSimilarity'. If they have, it returns the similarity of both tests
// if they don't have returns -1.
func IsSimilarFlake(test1, test2 Test, flakeSimilarity float64) float64 {
	// If the test name is not the same then do not even
	// bother checking for string similarity.
	testName := test1.TestName
	if testName != test2.TestName {
		return -1
	}
	if len(test1.FailureOutput) == 0 {
		return -1
	}
	failSim := stringSim(test1.FailureOutput, test2.FailureOutput)
	traceSim := stringSim(test1.StackTrace, test2.StackTrace)
	sim := failSim * traceSim
	if sim >= flakeSimilarity {
		return sim
	}
	return -1
}

// stringSim returns percentage of similarity that 2 strings have between each
// other. Returns 0 if they are not considered similar and 1 if they are equal.
// The algorithm behind this function is extremely simple and if 2 strings do
// not start with the same words, although they might look similar, the function
// can return a value away from 1. For example:
// stringSim("abc", "abc") // returns 1.00
// stringSim("abc", "zabc") // returns 0.43
func stringSim(a, b string) float64 {
	dmp := diffmatchpatch.New()

	diffs := dmp.DiffMain(a, b, false)

	return 1 - float64(len(diffs))/float64((len(a)+len(b))/2)
}

// CheckGHIssuesFailures checks if the given testFailure is similar to any of
// the GH issues from 'issueJenkinsURLFails'. If found, returns the issue number
// and its similarity.
func CheckGHIssuesFailures(issueJenkinsURLFails map[int]GHIssue, testFailure Test, flakeSimilarity float64) (int, float64) {
	maxGHIssueSimilarity := flakeSimilarity
	maxGHIssueNumber := -1
	for ghIssueNumber, ghIssue := range issueJenkinsURLFails {
		// If the test name is not the same then do not even
		// bother checking test similarity.
		testName := ghIssue.TestName
		if len(testName) == 0 {
			testName = strings.Replace(ghIssue.Title, "CI: ", "", 1)
		}
		sim := IsSimilarFlake(ghIssue.Test, testFailure, flakeSimilarity)
		if sim > maxGHIssueSimilarity {
			maxGHIssueSimilarity = sim
			maxGHIssueNumber = ghIssueNumber
		}
	}
	if maxGHIssueNumber == -1 {
		return -1, -1
	}
	return maxGHIssueNumber, maxGHIssueSimilarity
}

type FlakeConfig interface {
	GetMaxFlakesPerTest() int
	CommonFailure(str string) bool
}

// GetJobFrom returns all failures filtered by 'filterFlakes' from the given
// jenkins URL.
func (jc *JenkinsClient) GetJobFrom(ctx context.Context, jobName string,
	filterFlakes func(jobFailures []BuildFailure, jobFailURL string) []BuildFailure) (JenkinsFailures, error) {

	stableBrTestFails, err := jc.GetTestResults(ctx, jobName)
	if err != nil {
		return nil, fmt.Errorf("unable to get test results of job %s: %w", jobName, err)
	}

	for testIdx, stableBrTestFail := range stableBrTestFails {
		stableTestFailures := filterFlakes(stableBrTestFail, jobName)
		if len(stableTestFailures) == 0 {
			delete(stableBrTestFails, testIdx)
		} else {
			stableBrTestFails[testIdx] = stableTestFailures
		}
	}
	return stableBrTestFails, err
}

// FilterFlakes filters the given flakes accordingly the configuration set in
// flakeConfig. If the number of flakes is greater than the maximum possible
// flakes per test, it will print a warning message in the logs and return
// nil.
func FilterFlakes(log *zerolog.Logger, flakeCfg FlakeConfig, jobFailures []BuildFailure, jobFailURL string) []BuildFailure {
	var prFailures []BuildFailure
	for _, prFailure := range jobFailures {
		if flakeCfg.CommonFailure(prFailure.FailureOutput) {
			continue
		}
		prFailures = append(prFailures, prFailure)
	}

	// If one PR build has more than N failures then
	// something wrong is happening and we should ignore
	// such builds. Otherwise all we will have is false
	// positives and we will wrongly consider them as flakes.
	if len(prFailures) > flakeCfg.GetMaxFlakesPerTest() {
		log.Warn().Fields(
			map[string]interface{}{
				"test-run":            jobFailURL,
				"pr-failures":         len(prFailures),
				"job-failures":        len(jobFailures),
				"max-flakes-per-test": flakeCfg.GetMaxFlakesPerTest(),
			}).Msg("Test run has more failures than expected")
		return nil
	}
	return prFailures
}

func IsJenkinsFailure(state, description string) bool {
	return state == "failure" &&
		// Jenkins job has "Build finished. " in its description.
		description == "Build finished. "
}
