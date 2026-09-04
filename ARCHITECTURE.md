# ForgeGrid Architecture

## Overview

ForgeGrid is a Go-based local-network coordinator/worker system. The current binary has three entry surfaces:

1. `-mode coordinator`
2. `-mode worker`
3. the separate `agent-bridge` subcommand family

There is no current Hybrid mode.

## Coordinator

The coordinator:

- persists cluster state through `internal/store`;
- creates/reuses a self-signed TLS certificate and prints its SHA-256 fingerprint;
- serves an embedded static dashboard over HTTP(S);
- creates five-minute, one-time six-digit pairing codes;
- stores worker token hashes and hardware/status metadata;
- receives authenticated worker heartbeats;
- exposes test-job, job-list/action and manifest-dispatch HTTP endpoints;
- marks a worker offline after its heartbeat has been absent for more than 15 seconds.

The coordinator listens on `0.0.0.0:<port>` by default. This makes it reachable on interfaces allowed by the host/network configuration; it is not technically restricted to a specific LAN interface.

## Worker

On first pair, a worker needs an explicit coordinator address, pairing code and (unless `-insecure`) TLS fingerprint. There is no current UDP/multicast auto-discovery path.

After pairing, the worker persists:

- worker ID;
- token;
- coordinator URL;
- pinned fingerprint;
- node name;
- insecure/TLS mode.

The worker then:

- sends a heartbeat every five seconds;
- polls for pending jobs every two seconds;
- executes accepted jobs concurrently;
- posts job state/results/logs back to the coordinator;
- reloads saved credentials on later starts.

## Transport and authentication

Normal coordinator/worker traffic uses HTTPS unless `-insecure` is deliberately selected.

The worker configures TLS verification with the supplied certificate fingerprint. Paired-worker heartbeat, polling and job-status updates use bearer tokens.

Not every coordinator/operator endpoint has a separate authentication layer. See `SECURITY.md` before treating ForgeGrid as suitable for an untrusted network.

## Jobs

### Challenge jobs

`POST /api/jobs/test` creates a pending job containing a random challenge. A worker hashes the challenge with SHA-256 and posts the result. The coordinator independently computes the expected hash and rejects a mismatch.

### Manifest execution jobs

`POST /api/jobs/manifest` strictly parses YAML into:

- a project name;
- named tasks;
- requirements (`os`, `min_ram_gb`, `min_cores`);
- execution profile, arguments, environment and timeout;
- artefact patterns.

The current Director creates one job per task and assigns the **first online worker that satisfies the OS requirement**. Although RAM/core fields are parsed, `min_ram_gb` and `min_cores` are not yet enforced during worker selection.

## Execution engine

Current execution profiles are:

- `go`
- `node`
- `python`
- `test` (internal/testing profile using `echo`)

The worker resolves the profile executable from `PATH`, fixes the working directory to its ForgeGrid workspace, applies a timeout and captures stdout/stderr. Logs are capped at 2,000 lines plus a truncation marker.

The workspace path helper prevents a selected working path from escaping the configured workspace, but the spawned process itself runs with the worker process's OS permissions. This is **not a container, VM or full filesystem sandbox**.

Logs are not currently streamed by WebSocket. They are captured during execution and included in the worker's status/result update.

Manifest artefact patterns are represented in the schema, but the current worker execution path does not collect or upload artefacts.

## Cancellation and resilience

The data model and HTTP API include cancellation state and worker-side cancellation support. However, the current worker polling endpoint returns only `pending` jobs, so a coordinator-side cancellation of an already-running job is not a proven end-to-end cancellation mechanism in the current implementation.

Worker heartbeat loss marks the worker offline, but the current coordinator does not automatically fail/reassign an already-running job solely because the worker goes offline.

Saved identity reconnect is implemented.

## Dashboard

The current dashboard is static embedded HTML served from `internal/ui/dashboard`. It is not a Preact/TypeScript application in the current tree.

## AgentBridge

AgentBridge is a separate message-relay subsystem under `internal/agentbridge` and is invoked as:

```bash
forgegrid agent-bridge <command>
```

Current commands include:

- `serve`
- `rotate-tls`
- `register`
- `configure-client`
- `reset-client`
- `send`
- `inbox`
- `ack`
- `complete`
- `fail`

The relay has its own TLS certificate/store and agent-token authentication. AgentBridge moves structured messages; it does not itself execute arbitrary remote shell commands. Any external coding-agent automation that acts on a received message remains a separate trust/execution layer.

## Not currently implemented

The current source tree does not implement the older architecture-plan claims of:

- Hybrid coordinator+worker mode;
- UDP/multicast coordinator discovery;
- Mirror-mode project synchronisation;
- Git clone/fetch/worktree synchronisation;
- Godot-specific execution adapter/profile;
- WebSocket log streaming;
- artefact collection/upload;
- RAM/core-aware scheduling;
- a full OS sandbox for execute jobs.

`IMPLEMENTATION_PLAN.md` tracks the remaining work without presenting those items as shipped features.
