// Copyright 2021 Authors of Cilium
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

package jenkins

import (
	"strings"
)

type Test struct {
	TestName       string `json:"test-name"`
	FailureOutput  string `json:"failure-output"`
	StackTrace     string `json:"stack-trace"`
	StandardOutput string `json:"standard-output"`
	StandardError  string `json:"standard-error"`
}

type GHIssue struct {
	Title string `json:"title"`
	Test
}

func ParseGHIssue(title string, body string) GHIssue {
	findTextBlock := func(mlh, human string) string {
		txt := textBlockBetween(body, mlh, false)
		if len(txt) != 0 {
			return txt
		}
		return textBlockBetween(body, human, true)
		// TODO remove this leftover?
		// firstLine := strings.IndexRune(txt, '\n')
		// Remove title
		// return txt[firstLine+1:]
	}

	return GHIssue{
		Title: title,
		Test: Test{
			TestName:       findTextBlock(testNameMLH, testNameHuman),
			FailureOutput:  findTextBlock(failureOutputMLH, failureOutputHuman),
			StackTrace:     findTextBlock(stackTraceMLH, stackTraceHuman),
			StandardOutput: findTextBlock(stdOutHuman, stdOutHuman),
			StandardError:  findTextBlock(stdErrMLH, stdErrHuman),
		},
	}
}

func textBlockBetween(body, str string, skipFirst bool) string {
	lines := strings.Split(body, "\n")
	beginning, end := -1, -1
	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == str {
			beginning = idx
		}
		if beginning != -1 && line == "```" {
			if skipFirst {
				skipFirst = !skipFirst
				continue
			}
			end = idx
			break
		}
	}
	if beginning == end {
		return ""
	}
	if end == -1 {
		end = len(lines)
	}
	return strings.Join(lines[beginning+1:end], "\n")
}
