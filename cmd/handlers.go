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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cilium/github-actions/pkg/github"
	gh "github.com/google/go-github/v71/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
)

type PRCommentHandler struct {
	githubapp.ClientCreator
}

func (h *PRCommentHandler) Handles() []string {
	return []string{"pull_request", "pull_request_review", "status", "check_run", "issue_comment"}
}

func (h *PRCommentHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var err error
	switch eventType {
	case "status":
		err = h.HandleStatusEvent(ctx, payload)
	case "check_run":
		err = h.HandleCheckRunEvent(ctx, payload)
	case "pull_request_review":
		err = h.HandlePullRequestReviewEvent(ctx, payload)
	case "pull_request":
		err = h.HandlePullRequestEvent(ctx, payload)
	case "issue_comment":
		err = h.HandleIssueCommentEvent(ctx, payload)
	}
	if err != nil {
		logger.Err(err).Msg("Unable to handle event")
		return fmt.Errorf("unable to handle event: %s\n", err)
	}

	return nil
}

func (h *PRCommentHandler) HandlePullRequestEvent(ctx context.Context, payload []byte) error {
	var event gh.PullRequestEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse pull request event payload")
	}
	installationID := event.GetInstallation().GetID()

	installClient, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	owner := event.PullRequest.Base.Repo.GetOwner().GetLogin()
	repoName := event.PullRequest.Base.Repo.GetName()
	ghClient := github.NewClientFromGHClient(installClient, owner, repoName, zerolog.Ctx(ctx))
	ghSha := event.PullRequest.Base.GetSHA()

	actionCfgPath, cfgFile, err := github.GetActionsCfg(ghClient, owner, repoName, ghSha)
	if err != nil {
		return err
	}
	if actionCfgPath == "" {
		return fmt.Errorf("unable to find config files in sha %s", ghSha)
	}

	var c github.PRBlockerConfig
	err = yaml.Unmarshal(cfgFile, &c)
	if err != nil {
		return fmt.Errorf("unable to unmarshal config %q file: %s\n", actionCfgPath, err)
	}

	return ghClient.HandlePullRequestEvent(c, &event)
}

func (h *PRCommentHandler) HandleStatusEvent(ctx context.Context, payload []byte) error {
	var event gh.StatusEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse status event payload")
	}
	installationID := event.GetInstallation().GetID()

	installClient, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	owner := event.Repo.GetOwner().GetLogin()
	repoName := event.Repo.GetName()
	ghClient := github.NewClientFromGHClient(installClient, owner, repoName, zerolog.Ctx(ctx))
	ghSha := event.GetSHA()

	actionCfgPath, cfgFile, err := github.GetActionsCfg(ghClient, owner, repoName, ghSha)
	if err != nil {
		return err
	}
	if actionCfgPath == "" {
		return fmt.Errorf("unable to find config files in sha %s", ghSha)
	}

	var c github.PRBlockerConfig
	err = yaml.Unmarshal(cfgFile, &c)
	if err != nil {
		return fmt.Errorf("unable to unmarshal config %q file: %s\n", actionCfgPath, err)
	}

	return ghClient.HandleStatusEvent(c, &event)
}

func (h *PRCommentHandler) HandlePullRequestReviewEvent(ctx context.Context, payload []byte) error {
	var event gh.PullRequestReviewEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse pull request review event payload")
	}
	installationID := event.GetInstallation().GetID()

	installClient, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	owner := event.PullRequest.Base.Repo.GetOwner().GetLogin()
	repoName := event.PullRequest.Base.Repo.GetName()
	ghClient := github.NewClientFromGHClient(installClient, owner, repoName, zerolog.Ctx(ctx))
	ghSha := event.PullRequest.Base.GetSHA()

	actionCfgPath, cfgFile, err := github.GetActionsCfg(ghClient, owner, repoName, ghSha)
	if err != nil {
		return err
	}
	if actionCfgPath == "" {
		return fmt.Errorf("unable to find config files in sha %s", ghSha)
	}

	var c github.PRBlockerConfig
	err = yaml.Unmarshal(cfgFile, &c)
	if err != nil {
		return fmt.Errorf("unable to unmarshal config %q file: %s\n", actionCfgPath, err)
	}

	return ghClient.HandlePullRequestReviewEvent(c, &event)
}

func (h *PRCommentHandler) HandleIssueCommentEvent(ctx context.Context, payload []byte) error {
	var event gh.IssueCommentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse issue comment event payload")
	}

	// Ignore any issue event that's not from a PR
	if !event.GetIssue().IsPullRequest() {
		return nil
	}

	// We don't support modified comments, only newly created
	if !event.GetComment().GetCreatedAt().Equal(event.GetComment().GetUpdatedAt()) {
		zerolog.Ctx(ctx).Info().Msgf("Not a new comment: %s", event.GetComment().GetBody())
		return nil
	}

	body := event.GetComment().GetBody()

	switch github.IsMLHCommand(body) {
	case github.MLHCommandNewFlake:
	default:
		if len(body) >= 30 {
			body = body[:30]
		}
		zerolog.Ctx(ctx).Info().Fields(map[string]interface{}{"body": body}).Msg("Not a MLH command")
		return nil
	}

	jobName := strings.Replace(body, string(github.MLHCommandNewFlake), "", -1)

	if jobName == "" {
		return fmt.Errorf("empty job name: %s", body)
	}

	installationID := event.GetInstallation().GetID()

	installClient, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	owner := event.Repo.GetOwner().GetLogin()
	repoName := event.Repo.GetName()

	ghClient := github.NewClientFromGHClient(installClient, owner, repoName, zerolog.Ctx(ctx))

	prNumber := event.GetIssue().GetNumber()

	pr, _, err := ghClient.GHClient.PullRequests.Get(ctx, owner, repoName, prNumber)
	if err != nil {
		return err
	}

	ghSha := pr.GetBase().GetSHA()

	actionCfgPath, cfgFile, err := github.GetActionsCfg(ghClient, owner, repoName, ghSha)
	if err != nil {
		return err
	}
	if actionCfgPath == "" {
		return fmt.Errorf("unable to find config files in sha %s", ghSha)
	}

	var c github.PRBlockerConfig
	err = yaml.Unmarshal(cfgFile, &c)
	if err != nil {
		return fmt.Errorf("unable to unmarshal config %q file: %s\n", actionCfgPath, err)
	}

	return ghClient.HandleIssueCommentEvent(ctx, c.FlakeTracker, jobName, pr, &event)
}

func (h *PRCommentHandler) HandleCheckRunEvent(ctx context.Context, payload []byte) error {
	var event gh.CheckRunEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse check run event payload")
	}

	if event.GetAction() != "completed" {
		return nil
	}

	installationID := event.GetInstallation().GetID()

	installClient, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	owner := event.Repo.GetOwner().GetLogin()
	repoName := event.Repo.GetName()
	ghClient := github.NewClientFromGHClient(installClient, owner, repoName, zerolog.Ctx(ctx))
	ghSha := event.GetCheckRun().GetHeadSHA()

	actionCfgPath, cfgFile, err := github.GetActionsCfg(ghClient, owner, repoName, ghSha)
	if err != nil {
		return err
	}
	if actionCfgPath == "" {
		return fmt.Errorf("unable to find config files in sha %s", ghSha)
	}

	var c github.PRBlockerConfig
	err = yaml.Unmarshal(cfgFile, &c)
	if err != nil {
		return fmt.Errorf("unable to unmarshal config %q file: %s\n", actionCfgPath, err)
	}

	return ghClient.HandleCheckRunEvent(c, &event)
}
