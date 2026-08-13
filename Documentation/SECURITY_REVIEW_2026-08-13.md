# Security Review - 2026-08-13

## Current Safeguards

- Workers must pair with a one-time code and token-authenticated heartbeats/jobs.
- Worker transport pins the coordinator TLS certificate fingerprint.
- Repo-backed jobs require an exact worker-side repository allowlist.
- Git push requires both manifest intent and worker `-allow-push`/`FORGEGRID_ALLOW_PUSH=true`.
- Push-capable dashboard submissions require a browser confirmation.
- Workers advertise labels/capabilities; manifests can route AI and build jobs only to intended workers.
- Job execution uses named profiles rather than arbitrary shell commands from the coordinator.
- Environment variables passed to child processes are allowlisted by the executor.
- Workspace paths are constrained before local non-Git execution.
- Branch creation rejects existing local and remote branch names before worktree creation.

## Remaining Risks

- AI-agent profiles can modify files inside the cloned worktree. Keep them on trusted workers and review PRs before merging.
- GitHub credentials live outside ForgeGrid and must be scoped per worker/repository.
- Artifact collection uploads small files and compressed packages when they fit the controller cap; very large or incompressible builds still need external storage or a future chunked transfer protocol.
- Live log streaming is still polling/status-update based rather than a dedicated stream.
- Dashboard Basic Auth is suitable for a trusted LAN but should not be exposed to the internet.
- Physical Windows/Linux LAN verification is still required before unattended use.

## Required Operator Rules

- Do not run workers with broad repo allowlists.
- Do not enable `-allow-push` on low-trust machines.
- Use unique `forgegrid/...` branch names per job.
- Use pinned commit SHAs for `repository.base_commit` on important jobs.
- Never paste secrets into manifests or dashboard message fields.
- Treat pushed branches as draft output until reviewed.
