---
name: test-first
description: Requires a real, failing test to exist before implementing a fix or feature. If something genuinely cannot be automated, explicit case-by-case approval is required.
---

# Test First

## Default workflow

For new behaviour or a bug fix:

1. Write or identify a test that exercises the required outcome.
2. Run it before implementation.
3. Confirm it fails for the expected reason.
4. Implement the smallest correct change.
5. Run the same test and confirm it passes.
6. Run relevant neighbouring and full-suite checks.
7. Preserve the regression test.

A syntax error, missing dependency, or unrelated setup failure is not a valid red phase.

## Meaningful assertions

A real test checks:

- exact return value
- expected error
- state transition
- persisted state
- file contents
- permission or ACL behaviour where executable
- process exit or survival
- network response
- absence of secret leakage
- concurrency invariant

These do not count:

- empty tests
- `pass`
- unconditional success
- sending a command without inspecting the result
- checking only that a file exists when its contents matter
- checking only that a process started when readiness matters

## Exceptions

When a test genuinely cannot be automated (e.g. physical LAN hardware, a human clicking through a Windows UI):

1. State explicitly as a named exception with a reason why an automated red-green test is unavailable.
2. Propose the strongest alternative evidence (static checks, cross-compilation, physical platform run, etc.).
3. Request case-by-case approval to proceed without an automated test.
4. Do not proceed until explicit approval is granted.

## Regression quality

A bug regression test must reproduce the original failure mode, not merely test a nearby happy path.

For concurrency defects, include repeated or race-enabled execution where appropriate.

For security defects, test rejection and cleanup paths as well as success.

## Reporting

Name each test and state exactly what it proves. Separate test coverage from untested assumptions.
