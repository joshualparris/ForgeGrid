---
name: security-review
description: Applies before and during changes involving authentication, network endpoints, dashboards, secrets, keys, permissions, process execution, shared state, or release packaging.
---

# Security Review

## Trigger

Use this skill for any change that:

- accepts network, file, process, or user input
- adds or changes an endpoint or dashboard action
- handles credentials, tokens, certificates, private keys, or encrypted bundles
- writes sensitive state
- executes commands or child processes
- changes permissions or ACLs
- changes release or packaging contents
- claims cancellation, revocation, isolation, locking, or atomicity

## Before implementation

State:

1. the trust boundary being crossed
2. the sensitive assets involved
3. the attacker or failure being considered
4. the files and interfaces expected to change
5. the security properties that must remain true

## Required checks

### Authentication and authorisation

- State-changing and administrative operations require appropriate authorisation.
- Worker credentials must not grant coordinator-admin authority unless deliberately designed.
- Registration, rotation, reset, and revocation behaviour must be explicit.
- Identity must not be silently overwritten.

### Input handling

- Limit request and file sizes before reading.
- Reject malformed, duplicate, trailing, or unknown input where strict schemas are expected.
- Validate identifiers, URLs, fingerprints, paths, timestamps, key sizes, nonce sizes, and state transitions.
- Never place untrusted strings into `innerHTML`, shell commands, paths, or logs without appropriate handling.

### Secrets

Check all possible exposure routes:

- command-line arguments
- environment variables
- temporary files
- stdout and stderr
- logs
- Git history and PR diffs
- release archives
- test artifacts
- crash output
- config files and backups

Use only dummy credentials in tests. Never commit or report live secrets.

### Platform-correct protection

- POSIX modes do not establish Windows ACLs.
- Windows secret storage must use verified ACLs or appropriate OS protection such as DPAPI.
- Apply protection before secret bytes are written, not afterward.
- Verify the final path after atomic rename.
- Fail closed when protection cannot be applied or verified.

### Concurrency and state

- Claim, reserve, consume, revoke, and transition operations must be atomic.
- Locks must have bounded acquisition and correct error handling.
- Persistence corruption must fail closed.
- Idempotency must not silently accept conflicting payloads.
- Save and rename behaviour must be reviewed per supported platform.

### Control semantics

A feature named cancel, stop, revoke, reset, drain, or disable must be verified to perform the underlying action, not only change displayed state.

### Network transport

- Use TLS where required.
- Verify pinning or certificate trust on the intended identity.
- Set HTTP server timeouts and request limits.
- Do not confuse encryption-to-a-recipient with authentication-of-a-sender.
- Rate limiting must not replace authentication.

## Required final report

Classify findings:

- **Blocker**: credible secret exposure, authentication bypass, unsafe remote execution, corruptible critical state, or false security claim
- **Important**: meaningful hardening or reliability issue that should be fixed before broad deployment
- **Minor**: maintainability or defence-in-depth improvement

Only blockers prevent approval unless the task specification defines a stricter gate.
