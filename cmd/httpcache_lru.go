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
	"github.com/gregjones/httpcache"
	lru "github.com/hashicorp/golang-lru"
)

// lruCache is a bounded httpcache.Cache backed by an LRU. The default
// httpcache.MemoryCache grows without bound: every distinct cached GitHub
// response is retained for the lifetime of the process, which on a small VM
// eventually drives the process into an out-of-memory condition. Capping the
// number of entries bounds memory at the cost of occasionally re-fetching an
// evicted (least-recently-used) response.
//
// The underlying golang-lru cache is safe for concurrent use, so no additional
// locking is required here.
type lruCache struct {
	c *lru.Cache
}

// newLRUCache returns an httpcache.Cache that retains at most size entries.
func newLRUCache(size int) httpcache.Cache {
	// lru.New only errors for a non-positive size; callers pass a validated
	// positive size, so a failure here is a programming error.
	c, err := lru.New(size)
	if err != nil {
		panic(err)
	}
	return &lruCache{c: c}
}

func (l *lruCache) Get(key string) ([]byte, bool) {
	v, ok := l.c.Get(key)
	if !ok {
		return nil, false
	}
	return v.([]byte), true
}

func (l *lruCache) Set(key string, responseBytes []byte) {
	l.c.Add(key, responseBytes)
}

func (l *lruCache) Delete(key string) {
	l.c.Remove(key)
}
