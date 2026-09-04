# ForgeGrid Source-Tree Status

**Documentation reviewed:** 4 September 2026

This file describes capabilities present in the current source tree. It is separate from the packaged `dist/ForgeGrid-USB` release snapshot, which includes its own dated binaries, checksums and historical test records.

## Working in current source

### Coordinator / worker core

- Coordinator and worker modes from one Go binary.
- Self-signed TLS coordinator with printed certificate fingerprint.
- Worker fingerprint pinning.
- Five-minute, one-time six-digit pairing codes.
- Per-worker tokens for heartbeat, job polling and job-result updates.
- Worker hardware inventory (OS/version, CPU, architecture, cores, RAM and free workspace disk).
- Persistent worker identity and reconnect.
- Embedded coordinator dashboard.
- Worker heartbeat/offline state.
- Persistent coordinator worker/job state.

### Jobs

- SHA-256 challenge test jobs with coordinator-side result checking.
- Strict YAML manifest parsing.
- Manifest task dispatch to online workers.
- Current OS requirement filtering.
- `go`, `node`, `python` and internal `test` execution profiles.
- Timeouts and bounded stdout/stderr capture.
- Job attempt IDs and status/result persistence.

### AgentBridge

- Separate HTTPS agent-message relay in the ForgeGrid binary.
- Agent registration and token authentication.
- TLS certificate generation/rotation.
- Pinned client configuration.
- Send, inbox, acknowledge, complete and fail message lifecycle.
- Windows client/bootstrap support material in `.agents` and `Documentation`.

## Implemented only partially

- **Manifest requirements:** `min_ram_gb` and `min_cores` are parsed, but the current Director selects workers using online state + OS only.
- **Cancellation:** cancellation data structures/API and worker context cancellation exist, but already-running cancellation is not yet a proven end-to-end path because workers poll pending jobs.
- **Workspace containment:** ForgeGrid constrains the execution working directory; this is not a full OS/process sandbox.
- **Artefacts:** manifest artefact patterns are parsed but the worker does not currently collect/upload them.
- **Logs:** stdout/stderr are captured while the command runs but sent with status/result updates rather than live WebSocket streaming.

## Not implemented in current source

- Hybrid coordinator+worker mode.
- UDP/multicast coordinator auto-discovery.
- Mirror-mode project synchronisation.
- Git-mode project synchronisation/worktrees.
- Godot-specific execution profile.
- RAM/core-aware scheduling.
- Artefact upload/collection.
- Complete retry mechanism.
- Full operator authentication for coordinator/dashboard controls.
- Full OS sandbox for job processes.

## Security status

Transport/authentication controls exist for worker communications, but the current coordinator should be treated as a **trusted-LAN development service**.

Important limitations documented in `SECURITY.md` include:

- the service listens on `0.0.0.0` rather than a single selected LAN interface;
- operator/dashboard control endpoints are not all protected by an admin authentication layer;
- worker bearer-token comparison is ordinary hash-string comparison, not the previously claimed dedicated constant-time comparison;
- execute-job logs do not have a general secret-redaction layer;
- allowed execution profiles are not equivalent to process sandboxing.

## Physical LAN evidence

The repository contains `dist/ForgeGrid-USB/Documentation/REAL_LAN_TEST_RESULTS_2026-08-04.md`.

That historical record supports:

- Fedora↔Windows pairing;
- heartbeat connectivity;
- Windows hardware reporting;
- saved-identity reconnect;
- two challenge jobs visibly starting on Windows.

It explicitly does **not** preserve enough coordinator-side evidence to independently prove those challenge jobs completed with accepted hashes. It also records that the then-current `VERIFY-WINDOWS.bat` printed success after an input-redirection error.

Do not describe the complete physical-Windows acceptance suite as passed on the strength of that record.

## Verification guidance

For current source changes:

```bash
go test ./...
go build ./...
```

Changes to Windows startup, credentials, ACLs, TLS, process execution or AgentBridge bootstrap also need Windows-specific validation; ordinary Go unit tests are not a substitute for those OS-level checks.

## Release-bundle note

`dist/ForgeGrid-USB` is a packaged snapshot. Its documentation should remain consistent with the binaries and `CHECKSUMS.txt` in that bundle. Do not casually edit generated/package files to mirror later source docs without rebuilding and re-verifying the bundle.
