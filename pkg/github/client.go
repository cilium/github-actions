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
	"context"
	"fmt"

	gh "github.com/google/go-github/v35/github"
	"golang.org/x/oauth2"
)

func NewClient(ghToken string) *gh.Client {
	return gh.NewClient(
		oauth2.NewClient(
			context.Background(),
			oauth2.StaticTokenSource(
				&oauth2.Token{
					AccessToken: ghToken,
				},
			),
		),
	)
}

func GetConfigFile(ghClient *gh.Client, owner, repoName, file, sha string) ([]byte, error) {
	fileContent, _, _, err := ghClient.Repositories.GetContents(
		context.Background(),
		owner,
		repoName,
		file,
		&gh.RepositoryContentGetOptions{Ref: sha})

	if err != nil {
		return nil, fmt.Errorf("unable to get configuration file %q: %w", file, err)
	}
	content, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("unable to load configuration file %q: %w", file, err)
	}

	return []byte(content), nil
}
