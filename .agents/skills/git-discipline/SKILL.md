---
name: git-discipline
description: Enforces focused branches, reviewable commits, safe pushes, and accurate Git reporting for any task involving version control.
---

# Git Discipline

## One task, one change stream

A task should use one clearly named branch and one focused commit, or a small number of logically separated commits when that improves review.

Do not mix unrelated cleanup, generated artifacts, experiments, or other tickets into the same branch.

## Before editing

Confirm:

- current repository
- current branch
- expected base branch
- working-tree state
- whether the user authorised branch creation, commit, push, and PR creation

Never assume these write actions are authorised.

## Branch rules

- Never commit directly to `main` or `master`.
- Use an existing task branch when one was supplied.
- Do not invent additional branches to bypass a blocked task.
- A validation branch targeting a feature branch must remain separate from `main` until reviewed.

## Before commit

Run and inspect:

```bash
git status --short
git diff --check
git diff --stat
git diff --name-only
git diff
```

Confirm:

- only expected files changed
- no credentials or private keys are present
- no compiled binaries or scratch files are staged
- tests were run on the files being committed
- generated files are intentional

Review the staged diff separately:

```bash
git diff --cached --name-only
git diff --cached
```

## Commit messages

Use an imperative message that states the actual change.

Avoid:

- final
- complete
- fully fixed
- working
- secure

unless the completion-verification requirements genuinely support the claim.

## Before push

- Fetch the remote.
- Confirm the branch has the intended upstream.
- Do not force-push unless explicitly approved.
- Do not push live secrets, bundles, local identities, token files, private keys, build output, or scratch evidence.

## After push

Verify the push rather than trusting command narration:

```bash
git rev-parse HEAD
git ls-remote origin <branch>
git status --short
```

When a PR exists, verify its head and base through GitHub tooling.

## PR content

A PR description should state:

- what changed
- why
- exact test evidence
- platform limitations
- security limitations
- what was not tested
- merge order when the PR targets another feature branch

Never equate GitHub's `mergeable` flag with approval or safety.
