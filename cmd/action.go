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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	gh "github.com/google/go-github/v30/github"
	"github.com/gregjones/httpcache"
	"github.com/palantir/go-baseapp/baseapp"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"goji.io/pat"
	"gopkg.in/yaml.v2"

	actions "github.com/cilium/github-actions/pkg"
	"github.com/cilium/github-actions/pkg/github"
)

var logger = zerolog.New(os.Stdout).With().Timestamp().Logger()

func main() {
	port, err := strconv.ParseUint(os.Getenv("LISTEN_PORT"), 10, 16)
	if err != nil {
		panic(err)
	}
	config := Config{
		Server: baseapp.HTTPConfig{
			Address: os.Getenv("LISTEN_ADDRESS"),
			Port:    int(port),
		},
	}
	config.Github.SetValuesFromEnv("")
	config.Github.App.PrivateKey = strings.Join(strings.Split(config.Github.App.PrivateKey, "\\n"), "\n")

	server, err := baseapp.NewServer(
		config.Server,
		baseapp.DefaultParams(logger, "maintainers-little-helper.")...,
	)
	if err != nil {
		panic(err)
	}

	cc, err := githubapp.NewDefaultCachingClientCreator(
		config.Github,
		githubapp.WithClientUserAgent("maintainers-little-helper/0.0.1"),
		githubapp.WithClientCaching(false, func() httpcache.Cache { return httpcache.NewMemoryCache() }),
		githubapp.WithClientMiddleware(
			githubapp.ClientMetrics(server.Registry()),
		),
	)
	if err != nil {
		panic(err)
	}

	prCommentHandler := &PRCommentHandler{
		ClientCreator: cc,
	}

	webhookHandler := githubapp.NewDefaultEventDispatcher(config.Github, prCommentHandler)
	server.Mux().Handle(pat.Post(githubapp.DefaultWebhookRoute), webhookHandler)
	server.Mux().HandleFunc(pat.Get("/healthz"), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Start is blocking
	err = server.Start()
	if err != nil {
		panic(err)
	}
}

type PRCommentHandler struct {
	githubapp.ClientCreator
}

func (h *PRCommentHandler) Handles() []string {
	return []string{"pull_request"}
}

func (h *PRCommentHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var event gh.PullRequestEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse issue comment event payload")
	}
	installationID := event.GetInstallation().GetID()

	ghClient, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}
	actionCfgPath := os.Getenv("CONFIG_PATH")
	owner := event.PullRequest.Base.Repo.GetOwner().GetLogin()
	repoName := event.PullRequest.Base.Repo.GetName()
	ghSha := event.PullRequest.Base.GetSHA()
	cfgFile, err := github.GetConfigFile(ghClient, owner, repoName, actionCfgPath, ghSha)
	if err != nil {
		return fmt.Errorf("unable to get config %q file: %s\n", actionCfgPath, err)
	}
	var c actions.PRBlockerConfig
	err = yaml.Unmarshal(cfgFile, &c)
	if err != nil {
		return fmt.Errorf("unable to unmarshal config %q file: %s\n", actionCfgPath, err)
	}
	l := actions.NewClient(ghClient, zerolog.Ctx(ctx))
	err = l.HandlePRE(c, &event)
	if err != nil {
		logger.Err(err).Msg("Unable to handle event")
		return fmt.Errorf("unable to handle PullRequestEvent: %s\n", err)
	}

	return nil
}
