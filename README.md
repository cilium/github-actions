# cilium-actions

This repository contains the logic behind the GitHub actions being executed
in `github.com/cilium/cilium`

## Configuration

Configuration needs to located at `.github/cilium-actions.yml`.

All the supported options are:

```yaml
# If project and column are set, all open and re-open PRs are automatically
# added to this project.
project: "https://github.com/cilium/cilium/projects/80"
column: "In progress"
# Move To Projects For Labels XORed will move PR for the project and column
# depending which of the labels are set. If 2 or more labels are set for the
# same branch, for example if `needs-backport/1.6` and `backport-pending/1.6`
# are set, no action will be performed.
move-to-projects-for-labels-xored:
  v1.6:
    needs-backport/1.6:
      project: "https://github.com/cilium/cilium/projects/1"
      column: "Needs backport from master"
    backport-pending/1.6:
      project: "https://github.com/cilium/cilium/projects/1"
      column: "Backport pending to v1.6"
    backport-done/1.6:
      project: "https://github.com/cilium/cilium/projects/1"
      column: "Backport done to v1.6"
  v1.5:
    needs-backport/1.5:
      project: "https://github.com/cilium/cilium/projects/2"
      column: "Needs backport from master"
    backport-pending/1.5:
      project: "https://github.com/cilium/cilium/projects/2"
      column: "Backport pending to v1.5"
    backport-done/1.5:
      project: "https://github.com/cilium/cilium/projects/2"
      column: "Backport done to v1.5"
# Require msg to be presented in all commits from the given PR
require-msgs-in-commit:
  - msg: "Signed-off-by"
    # Helper message that will be set as a comment if the PR does not contain
    # a the required msg in the commit message.
    helper: "https://docs.cilium.io/en/stable/contributing/contributing/#developer-s-certificate-of-origin"
    # Labels that are set in the PR in case the msg does not exist in the commit.
    set-labels:
      - "dont-merge/needs-sign-off"
# Block mergeability of a PR by checking if a particular set of labels are set
# or are not set.
block-pr-with:
  labels-unset:
      # Regex for the labels that should be present.
    - regex-label: "release-note/.*"
      # Helper message that will be set as a comment if the PR does not contain
      # the regex label
      helper: "Release note label not set, please set the appropriate release note."
      set-labels:
      # Labels that will automatically be set in case the PR does not contain
      # a label that match the regex above.
        - "dont-merge/needs-release-note"
  labels-set:
    - regex-label: "dont-merge/.*"
      # Message that will be showed as part of the mergeability GitHub Checker
      # to inform users why the PR is not in a mergeable state.
      helper: "Blocking mergeability of PR as 'dont-merge/.*' labels are set"
# Automatically add these labels in case the PR is open or reopen
auto-label:
  - "kind/backports"
  - "backport/1.6"
```
