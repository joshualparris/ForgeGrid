# ForgeGrid

ForgeGrid is a portable local-network coordinator/worker system for distributing development jobs across Windows and Linux machines. The current implementation is a Go binary with an embedded dashboard, TLS-pinned worker pairing, persistent worker identity, hardware reporting, manifest-driven command execution, and a separate AgentBridge relay for agent-to-agent messages.

## Current capabilities

- **Coordinator and worker modes** — one binary runs as either a coordinator or a worker.
- **TLS by default** — the coordinator generates a self-signed certificate; workers pin the supplied SHA-256 fingerprint on first pairing.
- **Expiring pairing codes** — six-digit codes expire after five minutes and are invalidated after successful use.
- **Per-worker tokens** — paired workers receive a unique token used for authenticated heartbeat, job polling and job-status updates.
- **Hardware reporting** — workers report OS, architecture, CPU model, physical/logical cores, RAM and free workspace disk.
- **Credential persistence** — workers persist their coordinator URL, identity, token, node name and fingerprint locally so they can reconnect after restart.
- **Embedded dashboard** — the coordinator serves a static embedded web UI over HTTP(S) and opens it in the default browser.
- **Challenge test jobs** — the coordinator can dispatch SHA-256 challenge jobs and validates the worker's returned hash.
- **Manifest execution** — strict YAML parsing creates jobs for named execution profiles (`go`, `node`, `python`, plus the internal `test` profile).
- **Timeouts and captured logs** — execute jobs run with a timeout and return bounded stdout/stderr logs with the final status update.
- **AgentBridge** — `forgegrid agent-bridge` provides an authenticated HTTPS message relay for coordinating external coding agents without adding remote-shell execution to the relay itself.
- **Portable release bundle** — `dist/ForgeGrid-USB` contains a built release snapshot for Windows/Linux plus its own checksums and release documentation.

## Important current limitations

The following ideas appear in older planning material but are **not** current ForgeGrid capabilities unless stated otherwise:

- no Hybrid coordinator+worker mode;
- no UDP/multicast coordinator auto-discovery — first pairing requires an explicit coordinator address;
- no Mirror-mode or Git-mode project synchronisation;
- no Godot-specific execution profile;
- no WebSocket/real-time log streaming — workers currently return captured logs through job status updates;
- manifest `min_ram_gb` and `min_cores` fields are parsed but the current Director only enforces the `os` requirement when choosing a worker;
- artefact patterns are parsed from manifests but artefact collection/upload is not implemented in the current execution path;
- process execution is given a workspace working directory and path checks, but it is **not a full OS sandbox**;
- coordinator/dashboard control endpoints are not currently protected by a separate operator-authentication layer. Treat the coordinator as a trusted-LAN development service, not an Internet-facing control plane.

See [SECURITY.md](SECURITY.md), [ARCHITECTURE.md](ARCHITECTURE.md) and [ACCEPTANCE_TESTS.md](ACCEPTANCE_TESTS.md) for the exact current boundary.

## Getting started

### Coordinator

```bash
./forgegrid -mode coordinator -port 8080
```

The coordinator prints its LAN IP and TLS fingerprint and opens the dashboard. Use `-insecure` only for development when you deliberately want HTTP instead of TLS.

### First-time worker pairing

Generate a pairing code from the coordinator, then run on the worker:

```powershell
.\ForgeGrid.exe -mode worker -name "Worker-1" -coordinator 192.168.1.10 -code 123456 -fingerprint <FP>
```

If the coordinator address has no port, workers default to port `8080`.

### Reconnect an already-paired worker

```powershell
.\ForgeGrid.exe -mode worker
```

Saved worker credentials are loaded automatically. To clear them and pair again:

```powershell
.\ForgeGrid.exe -mode worker -reset-worker
```

## Manifest jobs

The coordinator accepts a strict YAML manifest at `POST /api/jobs/manifest`.

Example shape:

```yaml
project: example
tasks:
  tests:
    description: Run tests
    requirements:
      os: windows
      min_ram_gb: 8
      min_cores: 4
    execution:
      profile: go
      args: ["test", "./..."]
      timeout_seconds: 300
    artefacts: []
```

Current behaviour to be aware of:

- `project` and at least one task are required;
- every task must specify an execution profile;
- unknown YAML fields are rejected;
- the current scheduler chooses the first online worker whose OS matches when an OS is specified;
- RAM/core requirements are not yet enforced by the Director;
- execution occurs in the worker's ForgeGrid workspace using a fixed allow-list of executable profiles;
- logs are capped and returned when job status is posted back to the coordinator.

## AgentBridge

AgentBridge is a separate local relay within the same binary:

```bash
./forgegrid agent-bridge serve --port 9090
./forgegrid agent-bridge register --name windows-agent
```

It supports registration, TLS rotation, secure client configuration, reset, send, inbox, acknowledgement, completion and failure reporting.

Read [Documentation/ANTIGRAVITY_AGENT_BRIDGE.md](Documentation/ANTIGRAVITY_AGENT_BRIDGE.md) before setting up a client.

## Development and verification

Run the Go test suite:

```bash
go test ./...
```

Build the current platform binary:

```bash
go build ./...
```

Windows-specific verification material is included in the release bundle under `dist/ForgeGrid-USB/Documentation` and `dist/ForgeGrid-USB/Windows`.

A historical real-LAN result from **4 August 2026** records successful Fedora↔Windows pairing, heartbeat and saved-identity reconnect, while also recording that coordinator-side completion evidence was not preserved and that the then-current Windows verification batch file falsely reported success after an error. That file is historical evidence, not a statement that all current acceptance criteria pass.

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) — current component and data-flow architecture.
- [SECURITY.md](SECURITY.md) — current threat model, guarantees and limitations.
- [ACCEPTANCE_TESTS.md](ACCEPTANCE_TESTS.md) — current implementation/acceptance matrix.
- [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) — implemented work versus remaining roadmap.
- [RELEASE_NOTES.md](RELEASE_NOTES.md) — current source-tree status and known gaps.
- [Documentation/ANTIGRAVITY_AGENT_BRIDGE.md](Documentation/ANTIGRAVITY_AGENT_BRIDGE.md) — AgentBridge setup and trust boundary.

> **Release-bundle note:** files under `dist/ForgeGrid-USB` are a packaged release snapshot. Do not edit them independently of rebuilding the bundle and checksums; use the top-level source documentation above for the current `main` branch.
