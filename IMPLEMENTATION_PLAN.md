# ForgeGrid Implementation Status and Roadmap

The original phase plan pre-dated much of the current implementation and mixed shipped features with aspirations. This document now records what exists on the current source branch and what remains.

## Implemented foundation

### Core binary and UI

- Go project and single binary.
- Coordinator mode.
- Worker mode.
- Embedded static dashboard.
- Automatic browser opening for coordinator UI.
- Windows and Linux build/release material.

Not implemented from the original plan: Hybrid mode and the Preact/TypeScript first-run wizard.

### Networking, pairing and worker state

- Self-signed TLS generation.
- Worker TLS fingerprint pinning.
- Six-digit expiring pairing codes.
- Per-worker identity/token issuance.
- Persistent worker credentials/reconnect.
- Five-second worker heartbeats.
- Offline detection after missed heartbeats.
- Hardware reporting through `gopsutil`.

Not implemented from the original plan: UDP/multicast coordinator auto-discovery.

### Jobs and manifests

- Persistent jobs in coordinator store.
- Challenge test jobs with coordinator-side SHA-256 verification.
- Strict `forgegrid.yaml`-style manifest parsing.
- One job per manifest task.
- OS-based worker eligibility.
- `go`, `node`, `python` and test execution profiles.
- Command timeout handling.
- stdout/stderr capture and bounded returned logs.
- worker job polling and attempt IDs.

Partially implemented:

- cancellation state exists, but running-job cancellation is not yet proven end to end;
- manifest `min_ram_gb` and `min_cores` are parsed but not enforced;
- artefact patterns are parsed but not collected/uploaded.

### AgentBridge

Implemented as a separate subsystem in the ForgeGrid binary:

- HTTPS relay server;
- TLS rotation;
- agent registration with one-time token display;
- secure client configuration helpers;
- send/inbox/ack/complete/fail message lifecycle;
- Windows bootstrap/security helper work under `.agents/scripts`;
- polling workflow helpers.

AgentBridge is a message relay, not a remote shell. External coding agents decide what to do with received instructions.

## Remaining roadmap

### Priority 1 — make existing claims fully true

- Enforce `min_ram_gb` and `min_cores` in the Director.
- Make running-job cancellation reach and terminate the worker process reliably.
- Add an explicit retry path with attempt history.
- Preserve trustworthy coordinator-side evidence in physical Windows/LAN verification.
- Ensure verification scripts fail closed on command errors.
- Add operator authentication/authorisation for coordinator/dashboard control actions.

### Priority 2 — project/input delivery

Choose and implement the intended model rather than documenting both as already available:

- Mirror-mode differential file transfer; and/or
- Git clone/fetch/worktree-based project synchronisation.

Until one exists, ForgeGrid assumes the required project/workspace content is already present on the worker.

### Priority 3 — outputs and observability

- Artefact collection/upload from manifest patterns.
- Near-real-time log delivery if needed (WebSocket/SSE or another explicit mechanism).
- Clear job-attempt history in the dashboard.
- Better worker-selection diagnostics when no eligible worker exists.

### Priority 4 — execution hardening

- Dedicated low-privilege worker identity guidance/installer.
- Stronger process/filesystem isolation if untrusted code is a supported workload.
- Secret-safe job logging/redaction.
- Per-job environment policy and validation.
- Decide whether allowed profiles remain fixed or become signed/admin-configured.

### Priority 5 — richer scheduling and integrations

- RAM/core-aware scheduling.
- Optional user-defined labels/capabilities.
- Godot profile/integration only after generic execution/output handling is robust.
- Multi-stage/dependency-aware manifests if required.
- Better multi-worker balancing instead of first-eligible selection.

### Priority 6 — AgentBridge orchestration

- Complete reproducible Windows bootstrap flow.
- Validate unattended polling/scheduling without turning the relay into arbitrary remote execution.
- Add durable supervisor/reviewer hand-off patterns where an execution agent can be independently checked.
- Keep AgentBridge authentication/TLS lifecycle separate from main coordinator worker credentials.

## Explicitly not current features

Do not document these as shipped until the code and tests support them:

- Hybrid coordinator+worker mode;
- automatic coordinator discovery;
- Mirror or Git project sync;
- Preact first-run wizard;
- WebSocket log streaming;
- artefact upload/collection;
- RAM/core scheduling;
- Godot-specific execution;
- full OS sandboxing;
- complete operator authentication.

## Documentation rule

When a roadmap item lands, update `README.md`, `ARCHITECTURE.md`, `SECURITY.md`, `ACCEPTANCE_TESTS.md` and `RELEASE_NOTES.md` in the same change where practical. Physical/test status should only be promoted when evidence for that exact claim exists.
