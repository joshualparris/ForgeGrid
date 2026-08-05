---
name: docs-vs-code-sync
description: Keeps README, architecture, release notes, setup instructions, checklists, and reports aligned with code and evidence in the exact revision being documented.
---

# Documentation and Code Synchronisation

## Core rule

Documentation must distinguish:

- current implemented behaviour
- tested behaviour
- physically validated behaviour
- planned architecture
- known limitations
- historical results

Do not write aspirational design as present capability.

## Capability-claim audit

For every statement such as supports, securely stores, automatically discovers, schedules, synchronises, cancels, isolates, streams, or verifies:

1. Locate the exact code path.
2. Identify the test or physical evidence.
3. Check the behaviour exists in the current revision.
4. Qualify any platform or scale limitation.
5. Remove or reword unsupported claims.

## Preserve document purpose

Do not replace reusable instructions with one-time results.

Prefer separate files for:

- test procedure
- dated test results
- roadmap or architecture
- current release status
- known defects

## Evidence wording

Use evidence-calibrated language:

- observed on one Windows laptop
- cross-compiled successfully
- unit-tested on Linux
- coordinator-side completion output was not preserved
- multi-worker scaling was not tested
- planned but not implemented

Avoid unsupported terms:

- flawless
- complete validation
- secure by default
- all tests passed
- production-ready
- fully autonomous

## Setup instructions

Verify every:

- command
- flag
- path
- port
- filename
- environment variable
- expected output

against the current code and packaging.

## Documentation-only changes

A documentation correction may proceed without a red-green test when behaviour is not changed, but must still run appropriate checks such as:

- link and path review
- command verification where feasible
- `git diff --check`
- comparison against current code
- exact changed-file review

## Final report

List any code capability that remains documented as planned or pending. Never silently hide a mismatch.
