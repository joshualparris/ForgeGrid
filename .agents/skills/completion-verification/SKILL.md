---
name: completion-verification
description: Enforces evidence-based reporting before describing any coding task, feature, fix, branch, commit, or pull request as complete, ready, working, fixed, done, or verified.
---

# Completion Verification

## Core rule

Never describe work as complete, ready, working, fixed, done, verified, production-ready, or merge-ready unless every material claim has been demonstrated against the exact final state being reported.

Code existing is not proof. A command being started is not proof. A local edit is not proof that GitHub contains it.

## Required verification

Before making a completion claim:

1. **Verify the exact final revision**
   - Record `git rev-parse HEAD`.
   - Run tests after the final file edit and final commit.
   - When files changed after a test run, rerun the affected checks.
   - Do not use evidence from an earlier revision as proof for the current revision.

2. **Exercise the behaviour**
   - Run the feature end to end where reasonably possible.
   - Verify the observable result, not merely that the process launched.
   - For an API, inspect status and response.
   - For a CLI, inspect exit code and output.
   - For a file operation, inspect the resulting file and permissions.
   - For a UI, verify the resulting state rather than only page load.

3. **Use meaningful tests**
   - Tests must assert expected outcomes.
   - Empty tests, `pass`, unconditional success, or actions without state checks do not count.
   - A regression test must reproduce the original failure before the fix when reasonably automatable.

4. **Verify repository state**
   Report:
   - `git status --short`
   - `git diff --stat`
   - `git log -1 --oneline`
   - commit SHA
   - exact test, vet, lint, build, or smoke-test commands and exit codes
   - any remaining untracked or generated files

5. **Verify remote state when claiming submission**
   - Confirm the local SHA exists on the intended remote branch.
   - Confirm the pull request head SHA matches the reported SHA.
   - Confirm the pull request targets the intended base branch.
   - Confirm claimed removed files are absent from the actual remote PR diff.
   - Confirm CI results belong to that exact SHA.

6. **Distinguish levels of confidence**
   Use precise wording:
   - implemented but untested
   - unit-tested only
   - cross-built but not physically run
   - physically validated on Windows
   - independently reviewed at SHA `<sha>`
   - merge-ready after required checks

7. **State partial coverage plainly**
   Never turn:
   - one machine into multi-machine validation
   - a mocked test into physical validation
   - a cross-compile into runtime validation
   - local success into remote success
   - documentation into implementation

## Security evidence

For security-sensitive work, explicitly report relevant evidence:

- authentication and authorisation
- untrusted-input validation
- secret exposure through files, arguments, environment, output, logs, commits, or artifacts
- platform-correct file or directory protection
- atomic shared-state operations
- bounded retries and timeouts
- rollback and cleanup after failure
- whether cancellation actually terminates underlying work

## Secret-safe reporting

Evidence must never expose live credentials, private keys, bearer tokens, pairing codes, encrypted credential bundles, or sensitive environment contents.

Use synthetic values for tests. Redact secrets in logs. Report hashes or safe metadata only when required.

## Failure behaviour

When verification cannot be completed:

- do not weaken security or tests to make checks pass
- do not silently skip the check
- report the exact blocker
- identify the smallest next action needed
- use a qualified status rather than a completion claim
