# Migration from the AdventureText prototype

## Source and target

The frozen source is `C:\dev\GithubActions\AdventureTextClusterUSB`, checkpoint `361218a`. Its `MIGRATION_INVENTORY.md` records every file's disposition. The destination is this repository's existing executable, coordinator/dashboard, worker, and `dist/ForgeGrid-USB/` distribution.

The prototype's Go 1.26.5 compiler does not justify a product requirement change. ForgeGrid retains Go 1.22.1 because the migrated validation code needs no newer language or library feature. Builds may use a newer compatible compiler, while CI must continue validating the declared version.

## PR #7 audit and decision

PR #7 contains useful concepts and tests for strict YAML decoding, resource requirements, manifest-created jobs, scheduling, log streaming, cancellation, and artefact boundaries. It must not be merged or cherry-picked wholesale:

- It accepts coordinator-supplied Windows and Linux command strings and runs them through `cmd /c` and `sh -c`.
- No immutable local execution profile constrains the executable, arguments, environment, PATH, working directory, privilege, or maximum timeout.
- Cancellation uses `exec.CommandContext`, which does not reliably terminate descendants on Windows or Linux.
- It has no timeout ceiling, process-tree proof, atomic attempt claim, terminal-attempt duplicate prevention, or durable attempt identity.
- Assignment sets only `WorkerID`; it does not atomically claim or transition the job, so polling and restart behaviour can duplicate execution.
- Worker status updates accept broad state strings and do not enforce a complete transition graph.
- Its cancellation test accepts either failed or cancelled, sleeps for timing, and does not prove a spawned child terminates.
- Its integration test uses fixed ports and sleeps, and the PR's Linux/race CI fails while Windows CI was cancelled.

Decision: supersede PR #7 with this integration branch, reusing its safe manifest/scheduling intent but replacing raw commands with immutable local execution-profile IDs and safe parameter schemas. Do not duplicate its scheduler. PR #7 can later close with a link to the replacement PR and this audit.

## Integration sequence

1. Port strict task validation, dependency ordering, overlap detection, redaction, and runner identity into ForgeGrid packages.
2. Add versioned project configuration that selects local execution profiles without executable or PATH overrides.
3. Add `DispatchBackend` with GitHub Actions as the authoritative AI-task queue and a non-operational LAN placeholder.
4. Add durable canonical attempt IDs, atomic local claims, terminal-state retention, and explicit retry attempts.
5. Add OS-specific process-tree containment and tests proving parent and child termination.
6. Integrate project/task/run views into the existing coordinator dashboard.
7. Port the proven worktree/scope/Git lifecycle into generic fixture-repository tests and then the trusted workflow engine.
8. Add AdventureText's thin project config and workflow adapter. Keep its four proven lifecycle files until a new ForgeGrid-backed task creates an open draft PR.
9. Only after parity, remove duplicated generic AdventureText helpers through a reviewed infrastructure PR and build the final ForgeGrid USB distribution.

## Parity gate

Migration is not complete until ForgeGrid fetches the current base, rejects an existing task branch, creates the exact isolated branch, denies Codex Git lifecycle access, performs both scope gates and independent tests, detects tracked/untracked changes, stages validated files only, pushes only the task branch, creates but never merges a draft PR, uploads a sanitised report on all outcomes, and cleans without altering runner-local `main`.

The next physical proof must use a new task such as `help-command-001`; it must not repeat the inventory proof.
