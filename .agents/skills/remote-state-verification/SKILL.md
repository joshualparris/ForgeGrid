---
name: remote-state-verification
description: Verifies that claimed commits, branches, pull requests, changed files, and CI results actually exist on the remote at the exact reported SHA.
---

# Remote State Verification

## Trigger

Use whenever reporting that work was:

- committed
- pushed
- submitted
- included in a PR
- removed from a PR
- reviewed
- CI-tested
- ready to merge
- merged

## Local-to-remote identity

Record:

```bash
git rev-parse HEAD
git status --short
git branch --show-current
git ls-remote origin <branch>
```

The remote branch SHA must match local `HEAD` before saying pushed.

If the working tree contains uncommitted fixes, say local-only. Do not describe them as part of the PR.

## Pull-request verification

Use GitHub as the source of truth and verify:

- PR number and state
- head branch
- head SHA
- base branch
- changed-file list
- merged status
- update timestamp
- CI/check results for the head SHA

Do not rely only on the contributor's terminal summary.

## Diff verification

When claiming a file is absent, restored, or removed:

- inspect the remote PR changed-file list
- inspect the remote patch where material
- compare against the PR base, not against local `HEAD`
- remember that restoring a binary from branch `HEAD` does not remove it when that branch already contains the changed binary

## CI verification

- A green run on another SHA is not evidence for the current SHA.
- A workflow configured only for PRs to `main` may not run for a PR targeting a feature branch.
- Empty status results mean there is no verified CI evidence, not success.
- Cross-compilation is not physical platform execution.

## Review verification

An independent review must name the exact SHA reviewed. A new push invalidates approval of the earlier SHA unless the reviewer explicitly reviews the new delta.

## Merge verification

After a merge:

- confirm the PR shows merged
- record the merge or squash commit
- confirm `main` contains the change
- distinguish merged feature-branch PRs from changes merged into `main`

## Failure language

When remote evidence is missing, say:

- committed locally but not pushed
- remote branch does not match local SHA
- PR head has not updated
- CI has not run for this SHA
- mergeability is reported, but review approval is still pending
