---
name: independent-review
description: Defines a read-only, exact-SHA review process that separates confirmed blockers from important and minor findings without expanding task scope indefinitely.
---

# Independent Review

## Review target

A review must identify:

- repository
- PR or branch
- exact commit SHA
- intended base branch
- stated acceptance criteria

Review the exact SHA in a detached disposable worktree where possible.

Do not edit, commit, push, merge, generate live credentials, or change the contributor's branch during a read-only review.

## Review procedure

1. Confirm checked-out SHA.
2. Inspect changed files against the intended base.
3. Run formatting, vet, lint, unit, integration, race, and build checks that apply.
4. Cross-build supported platforms where useful.
5. Inspect security and failure paths directly.
6. Verify that test strength was not reduced.
7. Verify generated binaries, secrets, and scratch files are absent.
8. Compare documentation claims with code and evidence.
9. Record exact command outcomes.

## Evidence boundaries

Distinguish:

- confirmed by code inspection
- confirmed by automated execution
- cross-built only
- requiring physical Windows validation
- requiring a real LAN test
- inference or theoretical concern

Do not claim to have physically validated another platform.

## Finding severity

### Blocker

Use only for issues that should prevent the stated merge, such as:

- credible credential or private-key exposure
- authentication or authorisation bypass
- arbitrary remote execution
- critical state corruption or replay failure
- silently weakened acceptance tests
- false success reporting
- build failure on a supported target
- inability to clean up sensitive partial state

### Important

Meaningful reliability, hardening, maintainability, or operational issues that can be tracked separately when the agreed merge gate is satisfied.

### Minor

Style, wording, small defence-in-depth, or low-impact maintainability issues.

## Scope control

Review against the agreed task and threat model. Do not repeatedly introduce unrelated redesign requirements.

After blockers are resolved:

- approve when the stated pass condition is met
- record important and minor findings as follow-up issues
- do not block indefinitely on theoretical perfection

## Final recommendation

Return exactly one:

- approve
- request changes

State the reviewed SHA and identify each blocking reason. A later push requires review of the new SHA or its delta.
