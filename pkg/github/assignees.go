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

	gh "github.com/google/go-github/v37/github"
)

type assignHandler func(ctx context.Context, owner, repo string, number int, assignees []string) (*gh.Issue, *gh.Response, error)

func (c *Client) manageAssignees(ctx context.Context, owner string, name string, number int, reviewers []*gh.User, handler assignHandler, msg string) (err error) {
	assignees := make([]string, 0, len(reviewers))
	defer func() {
		c.log.Info().Fields(map[string]interface{}{
			"error":     err,
			"assignees": assignees,
			"pr-number": number,
		}).Msg(msg)
	}()
	for _, user := range reviewers {
		assignees = append(assignees, user.GetLogin())
	}
	if len(assignees) == 0 {
		return nil
	}
	_, _, err = handler(
		ctx,
		owner,
		name,
		number,
		assignees,
	)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) Assign(ctx context.Context, owner string, name string, number int, users []*gh.User) (err error) {
	return c.manageAssignees(ctx, owner, name, number, users,
		c.GHCli.Issues.AddAssignees, "Added assignees to PR")
}

func (c *Client) Unassign(ctx context.Context, owner string, name string, number int, users []*gh.User) (err error) {
	return c.manageAssignees(ctx, owner, name, number, users,
		c.GHCli.Issues.RemoveAssignees, "Remove assignees from PR")
}
