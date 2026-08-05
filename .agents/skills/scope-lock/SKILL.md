---
name: scope-lock
description: Keeps a coding task within a declared set of files, behaviours, and outcomes, and prevents silent scope expansion during implementation.
---

# Scope Lock

## Establish scope

Before editing, identify:

- requested outcome
- in-scope files or directories
- allowed new files
- explicitly out-of-scope areas
- pass condition
- whether committing, pushing, or opening a PR is authorised

When the user supplies scope, preserve it exactly.

When no scope is supplied:

- propose the scope and obtain approval before editing

## Continuous enforcement

Before touching a file, ask:

1. Is it inside the declared scope?
2. Is the change required for the pass condition?
3. Does it introduce a new dependency, entry point, server, process, storage format, or public interface?
4. Does it alter tests or documentation outside the requested behaviour?

Stop and report before crossing scope. Do not silently broaden the task because a wider redesign seems cleaner.

## Prohibited workarounds

Unless explicitly approved, do not:

- create another `main()` or alternate server
- create a new branch or worktree
- push or open a PR
- weaken or delete acceptance tests
- replace a requested implementation with a side-channel script
- modify generated binaries
- edit unrelated documentation
- refactor neighbouring code merely for style

## Scope discoveries

When the correct fix genuinely requires an extra file:

- name the file
- explain why it is required
- explain the consequence of not touching it
- wait for approval when the expansion is material

Build-tag companions, tightly coupled tests, and minimal documentation corrections may be included when declared before the write.

## Final scope reconciliation

Report:

- original in-scope list
- actual changed-file list
- any authorised additions
- confirmation that no out-of-scope files changed

Use `git diff --name-only` and `git status --short`, not memory.
