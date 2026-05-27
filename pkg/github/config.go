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

package github

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	cache "github.com/patrickmn/go-cache"
)

type PRBlockerConfig struct {
	RequireMsgsInCommit []MsgInCommit `yaml:"require-msgs-in-commit,omitempty"`
	AutoLabel           []string      `yaml:"auto-label,omitempty"`
	BlockPRWith         BlockPRWith   `yaml:"block-pr-with,omitempty"`
	AutoMerge           AutoMerge     `yaml:"auto-merge,omitempty"`
	FlakeTracker        *FlakeConfig  `yaml:"flake-tracker,omitempty"`
}

// configCacheEntry is the cached resolution of GetActionsCfg for a given
// (owner, repo, sha) tuple. Because the cache key includes a commit SHA, the
// underlying content is immutable; the cached entry can never become stale
// for that key. New commits (with potentially different config) produce new
// keys and therefore trigger a fresh fetch.
//
// A zero-valued entry (path == "" && content == nil) represents the negative
// result "no config file exists at any of the configured paths for this SHA".
type configCacheEntry struct {
	path    string
	content []byte
}

const (
	// configCacheTTL bounds memory by aging out entries for long-abandoned
	// SHAs. Entries are always safe to keep longer (content is immutable),
	// so this is purely a memory hygiene knob.
	configCacheTTL          = 1 * time.Hour
	configCacheCleanupEvery = 10 * time.Minute
)

var (
	configCache = cache.New(configCacheTTL, configCacheCleanupEvery)

	// inflightMu and inflight implement a per-key single-flight: when many
	// webhooks for the same (owner, repo, sha) arrive concurrently, exactly
	// one of them fetches the config from GitHub and the rest wait on the
	// result. This eliminates the request stampede that trips GitHub's
	// secondary rate limiter.
	inflightMu sync.Mutex
	inflight   = map[string]*inflightCall{}
)

type inflightCall struct {
	done    chan struct{}
	path    string
	content []byte
	err     error
}

func configCacheKey(owner, repoName, ghSha string) string {
	return owner + "/" + repoName + "@" + ghSha
}

func GetActionsCfg(ghClient *Client, owner, repoName, ghSha string) (string, []byte, error) {
	key := configCacheKey(owner, repoName, ghSha)

	// Fast path: serve from cache.
	if v, ok := configCache.Get(key); ok {
		e := v.(configCacheEntry)
		return e.path, e.content, nil
	}

	// Slow path: single-flight the actual fetch so concurrent webhooks
	// for the same SHA collapse to one API call.
	inflightMu.Lock()
	if call, ok := inflight[key]; ok {
		inflightMu.Unlock()
		<-call.done
		return call.path, call.content, call.err
	}
	call := &inflightCall{done: make(chan struct{})}
	inflight[key] = call
	inflightMu.Unlock()

	defer func() {
		inflightMu.Lock()
		delete(inflight, key)
		inflightMu.Unlock()
		close(call.done)
	}()

	call.path, call.content, call.err = fetchActionsCfg(ghClient, owner, repoName, ghSha)
	if call.err == nil {
		// Cache both positive ("found at path X") and negative
		// ("no config at any path") resolutions. Both are safe to
		// cache because the SHA pins the content.
		configCache.SetDefault(key, configCacheEntry{path: call.path, content: call.content})
	}
	return call.path, call.content, call.err
}

func fetchActionsCfg(ghClient *Client, owner, repoName, ghSha string) (string, []byte, error) {
	actionCfgPath := os.Getenv("CONFIG_PATHS")
	configPaths := strings.Split(actionCfgPath, ",")
	for _, configPath := range configPaths {
		cfgFile, err := ghClient.GetConfigFile(owner, repoName, configPath, ghSha)
		switch {
		case IsNotFound(err) || IsNotFound(errors.Unwrap(err)):
			continue
		case err != nil:
			return "", nil, fmt.Errorf("unable to get config %q file: %s %T\n", configPath, err, errors.Unwrap(err))
		}
		return configPath, cfgFile, nil
	}
	return "", nil, nil
}
