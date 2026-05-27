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
	"errors"
	"fmt"
	"time"

	"github.com/cilium/github-actions/pkg/jenkins"
	gh "github.com/google/go-github/v84/github"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
)

// maxRateLimitRetries is the maximum number of times GetConfigFile will retry
// when it hits a primary or secondary GitHub rate limit.
const maxRateLimitRetries = 5

// maxRateLimitWait is the maximum amount of time GetConfigFile will sleep
// while waiting for a rate limit to reset before giving up.
const maxRateLimitWait = 2 * time.Minute

type Client struct {
	GHClient   *gh.Client
	log        *zerolog.Logger
	orgName    string
	repoName   string
	clientMode bool
}

func NewClient(ghToken string, orgName, repo string, logger *zerolog.Logger) *Client {
	return &Client{
		GHClient: gh.NewClient(
			oauth2.NewClient(
				context.Background(),
				oauth2.StaticTokenSource(
					&oauth2.Token{
						AccessToken: ghToken,
					},
				),
			),
		),
		orgName:    orgName,
		repoName:   repo,
		clientMode: true,
		log:        logger,
	}
}

func NewClientFromGHClient(ghClient *gh.Client, orgName, repo string, logger *zerolog.Logger) *Client {
	return &Client{
		GHClient: ghClient,
		log:      logger,
		orgName:  orgName,
		repoName: repo,
	}
}

func (c *Client) GetConfigFile(owner, repoName, file, sha string) ([]byte, error) {
	var (
		fileContent *gh.RepositoryContent
		err         error
	)
	for attempt := 0; attempt < maxRateLimitRetries; attempt++ {
		fileContent, _, _, err = c.GHClient.Repositories.GetContents(
			context.Background(),
			owner,
			repoName,
			file,
			&gh.RepositoryContentGetOptions{Ref: sha})
		if err == nil {
			break
		}

		wait, ok := rateLimitRetryAfter(err)
		if !ok {
			return nil, fmt.Errorf("unable to get configuration file %q: %w", file, err)
		}
		if wait > maxRateLimitWait {
			return nil, fmt.Errorf("unable to get configuration file %q: rate limit wait %s exceeds max %s: %w", file, wait, maxRateLimitWait, err)
		}
		if c.log != nil {
			c.log.Warn().
				Err(err).
				Str("file", file).
				Dur("retry_after", wait).
				Int("attempt", attempt+1).
				Msg("GitHub rate limit hit while fetching config file, retrying after wait")
		}
		time.Sleep(wait)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to get configuration file %q after %d retries: %w", file, maxRateLimitRetries, err)
	}
	content, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("unable to load configuration file %q: %w", file, err)
	}

	return []byte(content), nil
}

// rateLimitRetryAfter returns the duration to wait before retrying if err is a
// GitHub primary or secondary rate limit error. The boolean return value
// indicates whether the error is in fact a rate limit error.
func rateLimitRetryAfter(err error) (time.Duration, bool) {
	// Secondary (abuse) rate limit.
	var abuseErr *gh.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		if abuseErr.RetryAfter != nil && *abuseErr.RetryAfter > 0 {
			return *abuseErr.RetryAfter, true
		}
		// GitHub didn't provide a Retry-After header; back off conservatively.
		return time.Minute, true
	}
	// Primary rate limit.
	var rateErr *gh.RateLimitError
	if errors.As(err, &rateErr) {
		wait := time.Until(rateErr.Rate.Reset.Time)
		if wait <= 0 {
			wait = time.Second
		}
		return wait, true
	}
	return 0, false
}

// GetFailedJenkinsURLs returns a slice of URLs of tests that failed for the
// given commit SHA.
func (c *Client) GetFailedJenkinsURLs(parentCtx context.Context, owner, repoName string, sha string) ([]string, error) {

	var (
		cancels []context.CancelFunc
	)
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	var ciFailures []string
	nextPage := 0
	for {
		ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
		cancels = append(cancels, cancel)
		gs, resp, err := c.GHClient.Repositories.GetCombinedStatus(ctx, owner, repoName, sha, &gh.ListOptions{
			Page: nextPage,
		})
		if err != nil {
			return nil, err
		}
		for _, statuses := range gs.Statuses {
			if jenkins.IsJenkinsFailure(statuses.GetState(), statuses.GetDescription()) {
				ciFailures = append(ciFailures, statuses.GetTargetURL())
			}
		}
		nextPage = resp.NextPage
		if nextPage != 0 {
			continue
		}
		break
	}

	// GH actions
	// TODO detect GH flakes in GH actions
	// ctx, cancel = context.WithTimeout(parentCtx, 30*time.Second)
	// cancels = append(cancels, cancel)
	// nextPage = 0
	// for {
	// 	lc, resp, err := c.Checks.ListCheckRunsForRef(ctx, owner, repoName, sha, &gh.ListCheckRunsOptions{
	// 		Status: func() *string { a := "completed"; return &a }(),
	// 		ListOptions: gh.ListOptions{
	// 			Page: nextPage,
	// 		},
	// 	})
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	for _, cr := range lc.CheckRuns {
	// 		fmt.Printf("State (%s): %s\n", cr.GetName(), cr.GetStatus())
	// 	}
	//
	// 	nextPage = resp.NextPage
	//
	// 	if nextPage != 0 {
	// 		continue
	// 	}
	// 	break
	// }

	return ciFailures, nil
}

func (c *Client) Log() *zerolog.Logger {
	return c.log
}
