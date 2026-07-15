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
	"net/http"
	"os"
	"strconv"
	"strings"

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

	// Bound the HTTP response cache with an LRU. The default
	// httpcache.MemoryCache never evicts, so on this small VM it grows until the
	// process is OOM-killed. A per-client LRU caps memory instead.
	cacheSize := envInt("HTTP_CACHE_SIZE", 4096)
	cc, err := githubapp.NewDefaultCachingClientCreator(
		config.Github,
		githubapp.WithClientUserAgent("maintainers-little-helper/0.0.1"),
		githubapp.WithClientCaching(false, func() httpcache.Cache { return newLRUCache(cacheSize) }),
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

	// Handle webhooks asynchronously with a bounded worker pool instead of the
	// default synchronous scheduler. This decouples handler duration (which can
	// include GitHub rate-limit backoff) from the HTTP response: the dispatcher
	// validates the payload, enqueues it, and immediately ACKs GitHub, so the
	// load balancer never times out waiting on a slow handler. The fixed worker
	// count also caps how much work runs concurrently, bounding peak memory on
	// the (small) VM instead of letting every in-flight webhook pile up at once.
	queueSize := envInt("WEBHOOK_QUEUE_SIZE", 64)
	workers := envInt("WEBHOOK_WORKERS", 8)
	scheduler := githubapp.QueueAsyncScheduler(
		queueSize, workers,
		githubapp.WithSchedulingMetrics(server.Registry()),
	)
	webhookHandler := githubapp.NewEventDispatcher(
		[]githubapp.EventHandler{prCommentHandler},
		config.Github.App.WebhookSecret,
		githubapp.WithScheduler(scheduler),
	)
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

// envInt reads a positive integer from the environment variable named key,
// falling back to def when the variable is unset, empty, unparseable, or
// non-positive.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		logger.Warn().Str("env", key).Str("value", v).Int("default", def).
			Msg("invalid integer env var, using default")
		return def
	}
	return n
}
