# Implementation Plan

## Phase 1: Core Setup & UI Foundation
- **Go Project Init**: Create the basic module structure (`cmd`, `internal/node`, `internal/ui`).
- **First-Run Wizard UI**: Create the Preact/Vanilla TS setup Wizard (Choose mode, name device, workspace location).
- **Embedded Assets**: Hook up `go:embed` for the dashboard and wizard.

## Phase 2: Networking & Security
- **TLS Generation**: Write utilities to generate self-signed certs.
- **Node Discovery**: Implement UDP broadcast/multicast for local network discovery.
- **Pairing Protocol**: 6-digit code generation, exchange, and token issuance.
- **Heartbeat & Status**: Workers send regular 5-second heartbeats containing CPU/RAM/OS status.

## Phase 3: Project Sync
- **Mirror Mode**: Implement file hashing (size, mtime, SHA-256) and efficient transfer protocol (chunks).
- **Git Mode**: Add integration with local git client to clone/fetch.
- **Manifest Parsing**: Parse and validate `forgegrid.yaml` structure.

## Phase 4: Job Execution & Scheduling
- **Scheduler**: Implement multi-criteria job assignment (RAM, CPU, required OS/labels).
- **Process Execution**: Command runner for Windows and Linux. Headless Godot detection.
- **Isolation**: Worktree/Branch setup for AI agents.
- **Artefacts & Logs**: Stream stdout/stderr in real-time. Gather artefacts post-execution.

## Phase 5: Dashboard & UX
- **Dashboard View**: Real-time cluster status, job history, and node control (Disable, Drain, Retry).
- **Australian English**: Validate wording.
- **Browser Automation**: Open browser automatically on launch.

## Phase 6: QA & Packaging
- **Integration Tests**: Simulated workers, parallel jobs, disconnect/reconnect logic.
- **Build Scripts**: Produce x86-64 builds for Windows (`.exe`) and Linux (`forgegrid`), setup the USB structure (`dist/ForgeGrid-USB`).
