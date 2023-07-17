// Copyright 2019-2022 Authors of Cilium
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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ownerRepoFromRepositoryURL(t *testing.T) {
	type args struct {
		url string
	}
	tests := []struct {
		name      string
		args      args
		wantOwner string
		wantRepo  string
		wantErr   assert.ErrorAssertionFunc
	}{
		{
			name: "valid URL",
			args: args{
				url: "https://api.github.com/repos/cilium/cilium",
			},
			wantOwner: "cilium",
			wantRepo:  "cilium",
			wantErr:   assert.NoError,
		},
		{
			name: "invalid URL due to slash at the end",
			args: args{
				url: "https://api.github.com/repos/cilium/cilium/",
			},
			wantOwner: "",
			wantRepo:  "",
			wantErr:   assert.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOwner, gotRepo, err := ownerRepoFromRepositoryURL(tt.args.url)
			if !tt.wantErr(t, err, fmt.Sprintf("ownerRepoFromRepositoryURL(%v)", tt.args.url)) {
				return
			}
			assert.Equalf(t, tt.wantOwner, gotOwner, "ownerRepoFromRepositoryURL(%v)", tt.args.url)
			assert.Equalf(t, tt.wantRepo, gotRepo, "ownerRepoFromRepositoryURL(%v)", tt.args.url)
		})
	}
}
