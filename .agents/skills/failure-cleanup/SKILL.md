---
name: failure-cleanup
description: Ensures failures are bounded, surfaced, rolled back, and cleaned up without leaving locks, processes, credentials, temporary files, or corrupt state.
---

# Failure and Cleanup

## Core rule

Every operation that can partially succeed must define what happens after each failure point.

Happy-path success is insufficient when failure can leave:

- a live process
- a held lock
- a reserved ID
- a plaintext token
- a private-key file
- a partially written config
- a corrupt state file
- an acknowledged but unprocessed message
- a misleading success status

## Failure design

For each multi-step operation, identify:

1. resources acquired
2. persistent state written
3. secrets materialised
4. external processes started
5. network state changed
6. the commit point
7. rollback actions before the commit point
8. cleanup actions after the commit point

## Required properties

### Bounded waits

- retries must have a deadline or attempt limit
- retry only expected transient errors
- return unexpected errors immediately
- preserve the original error

### Locks

- acquire atomically
- release on every path after successful acquisition
- surface unlock failures
- when both operation and unlock fail, preserve both contexts
- never delete another process's active lock merely because it is old without a safe ownership protocol

### Files and secrets

- create protected empty files or directories before writing secrets
- write via secure temporary paths
- flush and close before rename
- remove temporary files on all failure paths
- fail closed on malformed or corrupt persistence
- do not claim overwriting guarantees secure erasure

### Processes

- capture exact PID
- verify the PID belongs to the process started
- stop only that process
- confirm it exited
- preserve unrelated processes
- remove PID files and handles

### State transitions

- reserve before action
- commit only after all required verification succeeds
- release reservation after failure
- make retries idempotent
- reject already-consumed operations
- do not acknowledge a queued task before an agent genuinely begins processing it

## Testing

Exercise:

- failure at each meaningful stage
- timeout
- unexpected lock error
- unlock error
- corrupt state
- process early exit
- occupied port
- permission or ACL failure
- config mismatch
- retry after rollback
- restart after committed state

Tests must assert no sensitive or operational residue remains.

## Reporting

Report cleanup evidence separately from functional success:

- process absent
- lock released
- temp files absent
- config absent or valid
- reservation released or consumed correctly
- no secret in output
- non-zero exit code on failure
