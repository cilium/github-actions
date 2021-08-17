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
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/bndr/gojenkins"
	"github.com/cilium/github-actions/pkg/progress"
	"golang.org/x/sync/semaphore"
)

var (
	// failRegex is used to find the test failure among Jenkins' output.
	failRegex = regexp.MustCompile(`FAIL:[^\n]+`)
)

const (
	statusFailureKeyword    = "FAILED"
	statusRegressionKeyword = "REGRESSION"
)

type JenkinsClient struct {
	*gojenkins.Jenkins
	serverMode bool
}

// NewJenkinsClient creates a new JenkinsClient. If 'serverMode' is set, UX log
// messages are not printed, for example loading bars.
func NewJenkinsClient(ctx context.Context, url string, serverMode bool) (*JenkinsClient, error) {
	jenkins := gojenkins.CreateJenkins(nil, url)
	_, err := jenkins.Init(ctx)
	if err != nil {
		return nil, err
	}

	return &JenkinsClient{
		Jenkins:    jenkins,
		serverMode: serverMode,
	}, nil
}

// JenkinsFailures maps a PR to a slice of BuildFailures
type JenkinsFailures map[int][]BuildFailure

type BuildFailure struct {
	BuildNumber int64  `json:"build-number"`
	JobName     string `json:"job-name"`
	// Artifacts is a list of URLs for such artifacts
	Artifacts []string `json:"artifacts"`
	// URL is the URL for the BuildFailure
	URL string `json:"url"`
	Test
}

// GetTestResults returns all test results of a job.
func (jc *JenkinsClient) GetTestResults(ctx context.Context, jobName string) (JenkinsFailures, error) {
	jbs, err := jc.Jenkins.GetAllBuildIds(ctx, jobName)
	if err != nil {
		return nil, err
	}

	var (
		wg             sync.WaitGroup
		mu             sync.Mutex
		errors         = make(chan error)
		jobCtx, cancel = context.WithCancel(ctx)
	)
	defer cancel()

	// let's do some concurrency since accessing jenkins sequentially is a
	// little slow
	sem := semaphore.NewWeighted(8)

	if !jc.serverMode {
		fmt.Printf(" found %d jobs!\n", len(jbs))
		progress.PrintLoadBar(0)
	}
	jenkinsFailures := JenkinsFailures{}

	wg.Add(len(jbs))

	for i, jb := range jbs {
		select {
		case <-jobCtx.Done():
			return nil, jobCtx.Err()
		case err := <-errors:
			return nil, err
		default:
		}

		if !jc.serverMode {
			progress.PrintLoadBar(float64(i) / float64(len(jbs)))
		}

		err := sem.Acquire(jobCtx, 1)
		if err != nil {
			return nil, err
		}
		go func(jobNumber int64) {
			defer sem.Release(1)
			defer wg.Done()
			prNumber, failures, err := jc.GetJobFailure(jobCtx, jobName, jobNumber)
			if err != nil {
				select {
				case errors <- err:
					return
				default:
					return
				}
			}
			if len(failures) == 0 {
				return
			}
			mu.Lock()
			jenkinsFailures[prNumber] = append(jenkinsFailures[prNumber], failures...)
			mu.Unlock()
		}(jb.Number)

	}
	wg.Wait()
	select {
	case <-jobCtx.Done():
		return nil, jobCtx.Err()
	case err := <-errors:
		return nil, err
	default:
	}
	return jenkinsFailures, nil
}

// GetJobFailure returns the PR number, or 0 if not found and a list of
// BuildFailures for the given buildNumber of the jobName. If the Build no
// longer exists no errors are returned and the list of BuildFailures is also
// nil.
func (jc *JenkinsClient) GetJobFailure(ctx context.Context, jobName string, buildNumber int64) (int, []BuildFailure, error) {
	build, err := jc.Jenkins.GetBuild(ctx, jobName, buildNumber)
	if err != nil {
		if err.Error() == "404" {
			return 0, nil, nil
		}
		return 0, nil, fmt.Errorf("unable to get build %d: %w", buildNumber, err)
	}
	// if pr is 0 then we can consider it is the 'master' branch or any job
	// that runs an a cron job.
	var pr int
	for _, actions := range build.Raw.Actions {
		for _, par := range actions.Parameters {
			if par.Name == "ghprbPullId" {
				prNumber, err := strconv.Atoi(par.Value)
				// There are at least 2 fields with this name. One has the PR
				// number as its value, the other has '${ghprbPullId}'. If we
				// can't parse '${ghprbPullId}' then it means it's not the field
				// we are looking for.
				if err != nil {
					continue
				}
				pr = prNumber
				break
			}
		}
		if pr != 0 {
			break
		}
	}

	rs, err := build.GetResultSet(ctx)
	if err != nil {
		if err.Error() == "404" {
			return 0, nil, nil
		}
		return 0, nil, fmt.Errorf("unable to get result set of build %d: %w", buildNumber, err)
	}

	// Get all artifact URLs.
	// We could download + upload them to GH but GH does not have that
	// functionality available as part of their API.
	// TODO: we could be smarter and detect which artifacts belong to which
	//  suite. Right now all artifacts are stored for all failures.
	var artifacts []string
	for _, artifact := range build.GetArtifacts() {
		artifacts = append(artifacts, jc.Server+artifact.Path)
	}

	var prFailures []BuildFailure
	for _, rss := range rs.Suites {
		for _, cas := range rss.Cases {
			if cas.Status != statusRegressionKeyword && cas.Status != statusFailureKeyword {
				continue
			}
			stdOut, _ := cas.Stdout.(string)
			stdErr, _ := cas.Stderr.(string)
			errStackTrace, _ := cas.ErrorStackTrace.(string)
			failed := failRegex.FindString(stdErr)
			prFailures = append(prFailures, BuildFailure{
				JobName:     jobName,
				BuildNumber: buildNumber,
				Artifacts:   artifacts,
				URL:         build.GetUrl(),
				Test: Test{
					TestName:       cas.Name,
					FailureOutput:  failed,
					StackTrace:     errStackTrace,
					StandardOutput: stdOut,
					StandardError:  stdErr,
				},
			})
		}
	}

	return pr, prFailures, nil
}

func SplitJobNameNumber(link string) (string, int64) {
	u, err := url.Parse(link)
	if err != nil {
		return "", -1
	}

	// Remove the `/job` prefix so that we have <jobName>/<number>
	// example of a u.Path: '/job/Cilium-PR-Runtime-4.9/5154/'
	nameNumber := strings.TrimPrefix(u.Path, "/job/")
	splitNameNumber := strings.Split(nameNumber, "/")
	if len(splitNameNumber) < 2 {
		return "", -1
	}
	jobNumber, err := strconv.ParseInt(splitNameNumber[1], 10, 64)
	if err != nil {
		return "", -1
	}
	return splitNameNumber[0], jobNumber
}
