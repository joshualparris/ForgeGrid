# ForgeGrid Security & Threat Model

## Scope

ForgeGrid executes development commands on paired worker machines. The current implementation has useful transport/authentication controls, but it is a **trusted-LAN development tool**, not a hardened remote-execution platform or Internet-facing control plane.

This document describes the current `main` implementation rather than the stronger security model proposed in older planning material.

## Current security controls

### TLS and certificate pinning

- Coordinator/worker HTTP traffic uses TLS by default.
- The coordinator generates and persists a self-signed certificate.
- First-time workers require the coordinator fingerprint unless `-insecure` is deliberately enabled.
- Workers use the pinned fingerprint when constructing their TLS client.

`-insecure` disables this protection and is for development only.

### Pairing codes

- Pairing codes are six digits.
- A generated code expires after five minutes.
- A successful pair invalidates the code.
- Repeated invalid pairing attempts are counted and eventually rejected until a new code is generated.

### Worker tokens

- Each paired worker receives a random token.
- The coordinator stores a SHA-256 hash of that token rather than the plaintext token.
- Heartbeats, worker job polling and worker job-status updates require the bearer token.

The current coordinator compares token-hash strings using ordinary Go string comparison. The older documentation claim of a dedicated constant-time token comparison was inaccurate.

### Request-size limits

Several coordinator endpoints limit request bodies, including pairing/heartbeat/test-job requests and larger job/log/manifest updates. This reduces accidental or trivial oversized-request pressure, but it is not a complete denial-of-service defence.

### Execution profile allow-list

Manifest jobs do not accept an arbitrary executable field. They reference a named execution profile. Current profiles resolve only:

- `go`
- `node`
- `python`
- `test` (`echo`, used for testing)

Arguments and environment values are still task-controlled, so an allowed interpreter/compiler can run powerful code. The profile allow-list must not be described as a sandbox.

### Workspace working-directory check

The worker resolves its execution working directory through `SecureWorkspacePath`, which rejects a selected path that escapes the configured workspace.

This constrains the **working directory**, not the operating-system permissions of the spawned process. Code run by Python/Node/Go can access anything the worker account itself can access unless the OS separately prevents it.

## Coordinator control-plane limitation

The current coordinator does **not** have a separate authenticated operator/admin session for all dashboard/control endpoints.

In particular, current source paths include unauthenticated operator-style endpoints for actions such as:

- generating a pairing code;
- listing workers/jobs from the dashboard side;
- creating test jobs;
- submitting a manifest;
- cancelling a job.

Worker-originated heartbeat/poll/result paths use worker tokens, but that does not protect the entire coordinator control plane from another network peer that can reach the service.

**Deployment consequence:** bind/firewall ForgeGrid to a network you trust. Do not expose the coordinator port directly to the public Internet or an untrusted shared network.

## Binding and network reachability

The coordinator currently listens on `0.0.0.0:<port>`. That means it accepts traffic on host interfaces permitted by the OS/firewall; it is not programmatically bound to one specific LAN interface.

The application is designed for LAN use, but host firewall/network configuration is part of the security boundary.

## Credential persistence

### Worker credentials

Workers persist plaintext JSON containing their worker ID, bearer token, coordinator URL, pinned fingerprint and node name in the platform data directory. The code requests restrictive Unix-style file modes, but Windows security ultimately depends on the filesystem/account ACLs in effect on that path.

Protect the user account and local credential file. If credentials may be exposed, use `-reset-worker` and pair again.

### AgentBridge client credentials

AgentBridge has a separate client configuration path. Its Windows helper applies a current-user ACL when writing the configuration. The token remains plaintext inside that ACL-protected file.

## Logs and secrets

The ForgeGrid execute-job path currently captures stdout/stderr and posts the captured log lines to the coordinator. There is **no general secret-redaction layer in the current execute-job log path**.

Do not print passwords, API keys or bearer tokens from jobs. Treat job logs and coordinator state as potentially sensitive.

The older claim that logs/environment secrets were automatically redacted was ahead of the implementation.

## Cancellation, isolation and destructive commands

The current implementation does not provide the previously documented policy engine for “destructive-looking” commands, confirmation prompts or command-specific allow-lists.

It also does not provide container/VM isolation. A job runs as the ForgeGrid worker OS account.

If you need stronger isolation, run the worker itself under a dedicated low-privilege OS account and/or inside an external sandbox/VM/container appropriate to the workload.

## AgentBridge boundary

AgentBridge is a separate authenticated HTTPS relay for agent messages. It deliberately does not implement arbitrary shell execution in the relay itself.

That is an important boundary, but any Antigravity/Claude/Codex automation that reads an AgentBridge message and then executes local actions is responsible for validating the instruction and enforcing its own permissions.

## Privacy and external dependencies

ForgeGrid does not contain product telemetry or a cloud backend in the current source tree. Core coordinator/worker communication is local HTTP(S).

The coordinator's LAN-IP helper attempts a UDP socket connection to `8.8.8.8:80` to infer the outbound local IP and falls back to `127.0.0.1` if that fails. No ForgeGrid application payload is sent by that helper, but the implementation should not be described as performing literally zero outbound network interaction.

## Current non-guarantees

ForgeGrid does **not** currently guarantee:

- authenticated operator access to every coordinator/dashboard action;
- protection against a hostile peer already on a reachable network;
- constant-time bearer-token comparison;
- arbitrary-command semantic safety;
- filesystem/process isolation beyond the worker account's OS permissions;
- automatic log secret redaction;
- artifact-content scanning;
- signed job results;
- mutually authenticated TLS client certificates;
- resistance to an administrator/root compromise of a worker or coordinator.

## Recommended deployment today

1. Use TLS; do not use `-insecure` outside isolated development/testing.
2. Verify the displayed coordinator fingerprint when first pairing a worker.
3. Keep coordinator ports behind a host firewall on a trusted LAN/VLAN.
4. Run workers with the least OS privilege their workloads need.
5. Do not put production secrets in job arguments, environment values or logs unless you have separately secured that path.
6. Reset/re-pair workers after suspected credential exposure.
7. Treat AgentBridge and ForgeGrid coordinator execution as separate trust surfaces.
8. Re-test Windows/Linux behaviour after changes to TLS, pairing, credentials, process execution or service startup.

Security hardening beyond these current controls belongs in the roadmap; documentation must not claim it before the code enforces it.
