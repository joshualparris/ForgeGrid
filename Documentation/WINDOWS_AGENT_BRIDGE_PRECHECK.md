# Windows AgentBridge Pre-Check — Historical Validation Record

> **Historical record, not current setup instructions.** This document captures the state of a specific Windows validation effort on branch `validation/windows-agentbridge-bootstrap` at commit `e03d09c6da4ab584b164d3f04850b0b3c385474a`. Current AgentBridge usage is documented in `ANTIGRAVITY_AGENT_BRIDGE.md`. Statements below describe what was observed/planned at that validation point unless explicitly marked as current-tree context.

## 1. Recorded repository state

- **Validation branch:** `validation/windows-agentbridge-bootstrap`
- **Base branch for PR:** `feature/secure-agent-bridge`
- **Commit SHA:** `e03d09c6da4ab584b164d3f04850b0b3c385474a`

These refs are provenance for the old test record; they are not the current documentation branch.

## 2. Recorded test results

- **Ordinary Windows tests:** **FAILED**
  - `TestMessageLifecycle` failed: `Windows should see 1 message, got 0`
  - `TestIntegrationConcurrent` failed: `Expected 700 messages, got 202`
- **Race test (`go test -race`):** unavailable in that Windows environment because the Go race build required CGO and a supported C toolchain.
- **Build (`go build`):** **PASSED** (exit code 0).

Do not convert this record into a current PASS. Re-run the present test suite on the current branch when Windows validation is required.

## 3. Configuration-path finding

Recorded defect:

- `internal/agentbridge/cli.go` placed Windows credentials under `%USERPROFILE%\.config\forgegrid`.

Recorded validation-branch fix:

- use `%LOCALAPPDATA%\ForgeGrid\agentclient.json` on Windows;
- apply a current-user ACL;
- store the token as plaintext protected by filesystem ACLs.

**Current-tree context:** the current AgentBridge CLI does use `%LOCALAPPDATA%\ForgeGrid\agentclient.json` on Windows and exposes `configure-client`/`reset-client`. See `ANTIGRAVITY_AGENT_BRIDGE.md` for current usage.

## 4. Recorded bootstrap design

The validation work used/planned:

- RSA 3072 with RSA-OAEP-SHA256;
- Ed25519 signatures;
- `.agents/scripts/bootstrap-crypto` for cryptographic operations;
- PowerShell wrappers for Windows DPAPI/ACL/cleanup handling.

Recorded bootstrap fingerprint:

`2515679f04ca9711e7dd88cabff84f3c341b1e0d8a88fb82f4607d2bdd7b3419`

That fingerprint belongs to the historical validation material. **Do not use it as the fingerprint for a current relay/client.** Use the fingerprint printed by the current AgentBridge relay/bootstrap flow you are actually configuring.

## 5. Polling helper record

At the time of this pre-check, the report recorded:

- `.agents/scripts/agentbridge-poll.ps1` as a local polling helper;
- acknowledgement-before-action behaviour;
- refusal to blindly execute arbitrary shell instructions;
- a task-scheduler helper/design that was intended to remain disabled until a manual round trip passed.

**Current-tree context:** `agentbridge-poll.ps1` is present in current `main`. A file named `.agents/scripts/install-poll-task.ps1` is not present in the current tree reviewed on 4 September 2026, so the older statement that this scheduler installer was implemented must not be treated as a current-file guarantee.

## 6. Historical remaining blockers

The validation record said the following were still required at that time:

- Fedora encryption/transmission of the registration bundle using the generated bootstrap public key;
- one successful manual round trip before unattended scheduling was enabled.

These are historical blockers, not a current completion checklist. Re-establish current state from the present code and a fresh end-to-end test.

## 7. Historical scheduling prompt

The old validation record proposed an Antigravity scheduled operation that would:

- open the ForgeGrid repository;
- read the agent workflow;
- poll the AgentBridge inbox;
- take at most one pending instruction;
- acknowledge it;
- validate scope/safety;
- perform allowed work;
- return completion/failure;
- never blindly execute arbitrary remote shell instructions.

The safety principles remain sensible, but the exact `/schedule` syntax and availability belong to the external Antigravity environment and are **not a ForgeGrid API contract**. Confirm the current agent product's scheduling syntax before using any automation prompt.

## What this document proves

This file proves only what the dated validation record says:

- a Windows build succeeded on the recorded branch/commit;
- two ordinary Windows tests were failing at that time;
- a credential-path defect and intended fix were identified;
- a bootstrap/polling design was under validation.

It does **not** prove current Windows tests pass, current unattended scheduling works, or current bootstrap deployment is complete.
