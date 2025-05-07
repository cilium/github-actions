// Copyright 2020-2021 Authors of Cilium
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
	"strings"
	"time"

	gh "github.com/google/go-github/v71/github"
)

type AutoMerge struct {
	Label            string `yaml:"label"`
	MinimalApprovals int    `yaml:"min-approvals"`
}

func (c *Client) AutoMerge(
	cfg AutoMerge,
	owner, repoName string,
	base,
	head *gh.PullRequestBranch,
	prNumber int,
	prLabels PRLabels,
	review *gh.PullRequestReview,
) error {

	ciChecks, err := c.getCIStatus(owner, repoName, base, head, prNumber)
	if err != nil {
		return err
	}

	if len(ciChecks) != 0 {
		c.log.Info().Fields(map[string]interface{}{
			"owner":     owner,
			"repo":      repoName,
			"pr-number": prNumber,
			"ci-checks": ciChecks,
		}).Msg("Not auto merging because ci failed")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	commit, _, err := c.GHClient.Repositories.GetCommit(ctx, owner, repoName, head.GetSHA(), &gh.ListOptions{})
	if err != nil {
		return err
	}

	commitDate := commit.GetCommit().GetCommitter().GetDate()
	if commitDate.IsZero() {
		c.log.Info().Fields(map[string]interface{}{
			"owner":       owner,
			"repo":        repoName,
			"pr-number":   prNumber,
			"sha":         head.GetSHA(),
			"commit":      gh.Stringify(commit.GetCommit()),
			"full-commit": gh.Stringify(commit),
		}).Msg("Not auto merging because of empty-committer")
		return nil
	}

	// If the CI have passed, check all reviews
	userReviews, err := c.getReviews(owner, repoName, prNumber)
	if err != nil {
		return err
	}
	if review != nil {
		// We have received a review event. We have the most updated review
		// from this user so we need to update it with the information that
		// we already have because GH might keep a cache of the previous review
		// done by that user.
		addReview(userReviews, review)
	}

	var (
		requestedReviews     []string
		approvals            int
		userChangesRequested = map[string]struct{}{}
	)
	for _, userReview := range userReviews {
		// request reviews for users that have stale reviews
		// (stale review is a review that was done before the PR was resynced
		// by the author)

		switch strings.ToLower(userReview.GetState()) {
		case "changes_requested":
			if userReview.SubmittedAt.Before(commitDate.Time) {
				requestedReviews = append(
					requestedReviews,
					userReview.GetUser().GetLogin(),
				)
			} else {
				userChangesRequested[userReview.GetUser().GetLogin()] = struct{}{}
			}
		case "approve", "approved":
			approvals++
		}
	}
	if len(requestedReviews) != 0 {
		c.log.Info().Fields(map[string]interface{}{
			"users-requested-changes": requestedReviews,
			"pr-number":               prNumber,
		}).Msg("Requesting reviews for users")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, _, err = c.GHClient.PullRequests.RequestReviewers(ctx, owner, repoName, prNumber, gh.ReviewersRequest{
			Reviewers: requestedReviews,
		})
		// We don't continue if we just have requested for new reviews
		return err
	}
	// Check if we still have pending reviewers
	users, teams, err := c.getPendingReviews(owner, repoName, prNumber)
	if err != nil {
		return err
	}
	// If the user has requested for changes we can delete them from here
	// because we are already waiting for a review from them.
	for user := range users {
		delete(userChangesRequested, user)
	}
	if review != nil {
		// We have received a review event. We have the most updated review
		// from this user so we need to update it with the information that
		// we already have because GH might keep a cache of the previous review
		// done by that user.
		switch strings.ToLower(review.GetState()) {
		case "changes_requested":
			userChangesRequested[review.GetUser().GetLogin()] = struct{}{}
		case "approve", "approved":
			delete(users, review.GetUser().GetLogin())
			delete(userChangesRequested, review.GetUser().GetLogin())
		}
	}

	if approvals < cfg.MinimalApprovals || len(users) != 0 || len(teams) != 0 || len(userChangesRequested) != 0 {
		c.log.Info().Fields(map[string]interface{}{
			"teams":                   teams,
			"users":                   users,
			"users-requested-changes": userChangesRequested,
			"min-approvals":           cfg.MinimalApprovals,
			"total-approvals":         approvals,
			"pr-number":               prNumber,
		}).Msg("Users have requested changes, the author hasn't synced the PR or the PR does not have the minimal approvals")
		// Only review the label if we know that exists or that we are handling
		// a PR review event (review != nil).
		if _, ok := prLabels[cfg.Label]; ok || review != nil {
			c.log.Info().Msg("Removing ready-to-merge label")
			_, err := c.GHClient.Issues.RemoveLabelForIssue(
				context.Background(), owner, repoName, prNumber, cfg.Label)
			if err != nil && !IsNotFound(err) {
				return err
			}
			delete(prLabels, cfg.Label)
		}
		return nil
	}

	if false {
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _, err = c.GHClient.Issues.CreateComment(ctx, owner, repoName, prNumber, &gh.IssueComment{
			Body: func() *string { a := fmt.Sprintf("Setting %s to let a human merge this PR.", cfg.Label); return &a }(),
		})
		if err != nil {
			return err
		}
	}

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, err = c.GHClient.Issues.AddLabelsToIssue(ctx, owner, repoName, prNumber, []string{cfg.Label})
	if err != nil {
		return err
	}
	c.log.Info().Fields(map[string]interface{}{
		"teams":                   teams,
		"users":                   users,
		"users-requested-changes": userChangesRequested,
		"min-approvals":           cfg.MinimalApprovals,
		"total-approvals":         approvals,
		"pr-number":               prNumber,
	}).Msg("Set 'ready-to-merge'")
	prLabels[cfg.Label] = struct{}{}

	return nil
}

// getCIStatus returns a list of CI Checks that are not successful.
func (c *Client) getCIStatus(
	owner, repoName string,
	base,
	head *gh.PullRequestBranch,
	prNumber int) ([]string, error) {

	var (
		cancels []context.CancelFunc
	)
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cancels = append(cancels, cancel)
	nextPage := 0

	brProt, _, err := c.GHClient.Repositories.GetBranchProtection(ctx, owner, repoName, base.GetRef())
	if err != nil {
		return nil, err
	}
	requiredContexts := map[string]struct{}{}
	for _, ctx := range *brProt.GetRequiredStatusChecks().Contexts {
		requiredContexts[ctx] = struct{}{}
	}

	passedContexts := map[string]bool{}

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		gs, resp, err := c.GHClient.Repositories.GetCombinedStatus(ctx, owner, repoName, head.GetSHA(), &gh.ListOptions{
			Page: nextPage,
		})
		if err != nil {
			return nil, err
		}
		for _, statuses := range gs.Statuses {
			if statuses.GetState() == "success" {
				if _, ok := passedContexts[statuses.GetContext()]; !ok {
					passedContexts[statuses.GetContext()] = true
				}
			}
		}
		nextPage = resp.NextPage
		if nextPage != 0 {
			continue
		}
		break
	}

	nextPage = 0
	for {
		lc, resp, err := c.GHClient.Checks.ListCheckRunsForRef(ctx, owner, repoName, head.GetSHA(), &gh.ListCheckRunsOptions{
			ListOptions: gh.ListOptions{
				Page: nextPage,
			},
		})
		if err != nil {
			return nil, err
		}
		for _, cr := range lc.CheckRuns {
			if cr.GetConclusion() == "success" || cr.GetConclusion() == "skipped" {
				if _, ok := passedContexts[cr.GetName()]; !ok {
					passedContexts[cr.GetName()] = true
				}
			} else {
				passedContexts[cr.GetName()] = false
			}
		}

		nextPage = resp.NextPage

		if nextPage != 0 {
			continue
		}
		break
	}

	for reqCtxName := range requiredContexts {
		// Remove the contexts that have passed from the requiredContexts
		if passedContexts[reqCtxName] {
			delete(requiredContexts, reqCtxName)
		}
	}

	ciChecks := make([]string, 0, len(requiredContexts))
	for reqCtxName := range requiredContexts {
		ciChecks = append(ciChecks, reqCtxName)
	}

	return ciChecks, nil
}

func addReview(recentReviewsByUser map[string]*gh.PullRequestReview, review *gh.PullRequestReview) {
	userName := review.GetUser().GetLogin()
	userReview, ok := recentReviewsByUser[userName]
	if !ok {
		recentReviewsByUser[userName] = review
		return
	}
	// We have the most up to date review from a user in in the
	// following conditions:
	//  CHANGES_REQUESTED overwrites any previous review
	//  APPROVE overwrites any previous review
	//  COMMENTED is only kept if no other APPROVE nor CHANGES_REQUESTED
	//  have been made
	if review.GetSubmittedAt().After(userReview.GetSubmittedAt().Time) {
		switch strings.ToLower(review.GetState()) {
		case "changes_requested", "approved":
			recentReviewsByUser[userName] = review
		}
	}
}

func (c *Client) getReviews(owner string, repoName string, prNumber int) (map[string]*gh.PullRequestReview, error) {
	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	recentReviewsByUser := map[string]*gh.PullRequestReview{}
	nextPage := 0
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)
		reviews, resp, err := c.GHClient.PullRequests.ListReviews(ctx, owner, repoName, prNumber, &gh.ListOptions{
			Page: nextPage,
		})

		if err != nil {
			return nil, err
		}
		for _, review := range reviews {
			addReview(recentReviewsByUser, review)
		}

		nextPage = resp.NextPage
		if nextPage != 0 {
			continue
		}
		break
	}
	return recentReviewsByUser, nil
}

func (c *Client) getPendingReviews(owner string, repoName string, prNumber int) (map[string]struct{}, map[string]struct{}, error) {
	nextPage := 0
	var (
		users   = map[string]struct{}{}
		teams   = map[string]struct{}{}
		cancels []context.CancelFunc
	)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancels = append(cancels, cancel)

		reviewers, resp, err := c.GHClient.PullRequests.ListReviewers(ctx, owner, repoName, prNumber, &gh.ListOptions{
			Page: nextPage,
		})
		if err != nil {
			return nil, nil, err
		}
		if len(reviewers.Teams) != 0 ||
			len(reviewers.Users) != 0 {

			for _, user := range reviewers.Users {
				users[user.GetLogin()] = struct{}{}
			}
			for _, team := range reviewers.Teams {
				teams[team.GetName()] = struct{}{}
			}

		}
		nextPage = resp.NextPage
		if nextPage != 0 {
			continue
		}
		break
	}

	return users, teams, nil
}
