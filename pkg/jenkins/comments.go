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
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

const (
	testNameMLH, testNameHuman           = "```test-name", "### Test Name"
	failureOutputMLH, failureOutputHuman = "```failure-output", "### Failure Output"
	stackTraceMLH, stackTraceHuman       = "```stack-trace", "### Stacktrace"
	stdOutMLH, stdOutHuman               = "```stack-output", "### Standard Output"
	stdErrMLH, stdErrHuman               = "```stack-error", "### Standard Error"
)

const (
	ghIssueComment = "PR #{{.PRNumber}} hit this flake with {{.Similarity}}% similarity:\n" +
		"<details><summary>Click to show.</summary>\n\n" +
		ghIssueDescription + "\n" +
		"</details>"

	ghPRCommentFail = "Job '{{.JobName}}' failed:\n" +
		"<details><summary>Click to show.</summary>\n\n" +
		testNameHuman + "\n" +
		testNameMLH + "\n" +
		"{{.TestName}}\n" +
		"```\n" +
		"\n" +
		failureOutputHuman + "\n" +
		failureOutputMLH + "\n" +
		"{{.FailureOutput}}\n" +
		"```\n" +
		"\n" +
		"</details>\n\n" +
		"Jenkins URL: {{.URL}}\n\n" +
		"If it is a flake and a GitHub issue doesn't already exist to track it, comment " +
		"`/mlh new-flake {{.JobName}}` so I can create one.\n\n" +
		"Then please upload the Jenkins artifacts to that issue."

	ghPRCommentKnownFlakes = "Job '{{.JobName}}' hit: {{range $key, $value := .Issues}} #{{$key}} ({{$value}}% similarity) {{end}}\n"

	ghPRCommentUnknownFlakes = "Job '{{.JobName}}' has {{ len .Failures }} failure{{.Plural}} " +
		"but they might be new flake{{.Plural}} since it also hit {{ len .Issues }} known " +
		"flake{{.Plural}}: {{range $key, $value := .Issues}} #{{$key}} ({{$value}}% similarity) {{end}}\n"

	ghIssueDescription = testNameHuman + "\n" +
		testNameMLH + "\n" +
		"{{.TestName}}\n" +
		"```\n" +
		"\n" +
		failureOutputHuman + "\n" +
		failureOutputMLH + "\n" +
		"{{.FailureOutput}}\n" +
		"```\n" +
		"\n" +
		stackTraceHuman + "\n" +
		"<details><summary>Click to show.</summary>\n\n" +
		stackTraceMLH + "\n" +
		"{{.StackTrace}}\n" +
		"```\n" +
		"</details>\n" +
		"\n" +
		stdOutHuman + "\n" +
		"<details><summary>Click to show.</summary>\n\n" +
		stdOutMLH + "\n" +
		"{{.StandardOutput}}\n" +
		"```\n" +
		"</details>\n" +
		"\n" +
		stdErrHuman + "\n" +
		"<details><summary>Click to show.</summary>\n\n" +
		stdErrMLH + "\n" +
		"{{.StandardError}}\n" +
		"```\n" +
		"</details>\n" +
		"\n" +
		"ZIP Links: \n" +
		"<details><summary>Click to show.</summary>\n\n" +
		"{{range $index, $element := .Artifacts}} {{$element}}\n {{end}}\n" +
		"</details>\n\n" +
		"Jenkins URL: {{.URL}}\n\n" +
		"If this is a duplicate of an existing flake, comment 'Duplicate of #\\<issue-number>' " +
		"and close this issue."

	ghPRCommentNewGHIssue = ":+1: created {{range $index, $value := .Issues}} #{{$value}} {{end}}\n"
)

// GHIssueComment returns an issue comment that can be used in GH issues
// to track which PRs are hitting certain flakes.
func GHIssueComment(prNumber int, similarity float64, failure BuildFailure) (string, string, error) {
	var tpl bytes.Buffer
	err := template.Must(template.New("gh-issue").Parse(ghIssueComment)).Execute(&tpl, struct {
		PRNumber   int
		Similarity string
		BuildFailure
	}{
		PRNumber:     prNumber,
		Similarity:   fmt.Sprintf("%.2f", similarity),
		BuildFailure: failure,
	})
	if err != nil {
		return "", "", err
	}

	title := fmt.Sprintf("CI: %s", failure.TestName)
	return title, tpl.String(), nil
}

// GHIssueDescription returns an issue description that can be used when
// creating a new GH issue flake.
func GHIssueDescription(failure BuildFailure) (string, string, error) {
	var tpl bytes.Buffer
	err := template.Must(template.New("gh-issue-description").Parse(ghIssueDescription)).Execute(&tpl, failure)
	if err != nil {
		return "", "", err
	}

	title := fmt.Sprintf("CI: %s", failure.TestName)
	return title, tpl.String(), nil
}

func PRCommentFailure(failure BuildFailure) (string, error) {
	var tpl bytes.Buffer
	err := template.Must(template.New("gh-comment-fail").Parse(ghPRCommentFail)).Execute(&tpl, failure)
	if err != nil {
		return "", err
	}

	return tpl.String(), nil
}

func PRCommentKnownFlakes(jobName string, issues map[int][]string) (string, error) {
	issuesFmt := make(map[int]string, len(issues))
	for issueNumber, sim := range issues {
		issuesFmt[issueNumber] = strings.Join(sim, "% similarity,")
	}

	var tpl bytes.Buffer
	err := template.Must(template.New("gh-comment-known-flake").Parse(ghPRCommentKnownFlakes)).Execute(&tpl, struct {
		Issues  map[int]string
		JobName string
	}{
		Issues:  issuesFmt,
		JobName: jobName,
	})
	if err != nil {
		return "", err
	}

	return tpl.String(), nil
}

func PRCommentUnknownFlakes(jobName string, failures []BuildFailure, issues map[int][]string) (string, error) {
	issuesFmt := make(map[int]string, len(issues))
	for issueNumber, sim := range issues {
		issuesFmt[issueNumber] = strings.Join(sim, "% similarity,")
	}
	var plural string
	if len(failures) > 1 {
		plural = "s"
	}
	var tpl bytes.Buffer
	err := template.Must(template.New("gh-comment-unknown-flake").Parse(ghPRCommentUnknownFlakes)).Execute(&tpl, struct {
		Failures []BuildFailure
		Plural   string
		Issues   map[int]string
		JobName  string
	}{
		Failures: failures,
		Plural:   plural,
		Issues:   issuesFmt,
		JobName:  jobName,
	})
	if err != nil {
		return "", err
	}

	return tpl.String(), nil
}

func PRCommentNewGHIssues(issues []int) (string, error) {
	var tpl bytes.Buffer
	err := template.Must(template.New("gh-comment-new-gh-issues").Parse(ghPRCommentNewGHIssue)).Execute(&tpl, struct {
		Issues []int
	}{
		Issues: issues,
	})
	if err != nil {
		return "", err
	}

	return tpl.String(), nil

}
