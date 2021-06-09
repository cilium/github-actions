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
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/cilium/github-actions/pkg/github"
	"github.com/gregjones/httpcache"
	"github.com/palantir/go-baseapp/baseapp"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/rs/zerolog"
	"goji.io/pat"
)

var logger = zerolog.New(os.Stdout).With().Timestamp().Logger()

func main() {
	if clientMode {
		runClient()
		return
	}

	runServer()
}

func runServer() {
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

func getActionsCfg(ghClient *github.Client, owner, repoName, ghSha string) (string, []byte, error) {
	actionCfgPath := os.Getenv("CONFIG_PATHS")
	configPaths := strings.Split(actionCfgPath, ",")
	for _, configPath := range configPaths {
		cfgFile, err := ghClient.GetConfigFile(owner, repoName, configPath, ghSha)
		switch {
		case github.IsNotFound(err) || github.IsNotFound(errors.Unwrap(err)):
			continue
		case err != nil:
			return "", nil, fmt.Errorf("unable to get config %q file: %s %T\n", configPath, err, errors.Unwrap(err))
		}
		return configPath, cfgFile, nil
	}
	return "", nil, nil
}
