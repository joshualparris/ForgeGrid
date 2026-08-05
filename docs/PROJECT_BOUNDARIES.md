# ForgeGrid project boundaries

## Product ownership

ForgeGrid is the reusable platform. It owns the single coordinator/dashboard, worker runtime, USB distribution, project and task validation, execution profiles, dispatch backends, trusted Git lifecycle, reporting, cancellation, redaction, installers, and fleet management.

AdventureText is the first private project using ForgeGrid. It owns only its game source, tests, documentation, `AGENTS.md`, CI, a versioned `.forgegrid/project.json`, and a thin workflow adapter. Its existing workflow and Python lifecycle helpers remain the physically proven reference until ForgeGrid passes parity tests and produces a new draft game pull request.

## What ForgeGrid already implements on `main`

- A Go 1.22.1 single binary with coordinator and worker modes.
- Embedded coordinator dashboard, persistent JSON storage, worker pairing, TLS certificate generation and fingerprint pinning.
- Worker inventory, heartbeats, a challenge-response test job, cancellation state, and a USB distribution under `dist/ForgeGrid-USB/`.
- Windows and Linux launch/verification assets and public GitHub-hosted CI.

Open work adds related capabilities:

- PR #3: secure AgentBridge transport and bootstrap primitives.
- PR #4: Windows AgentBridge bootstrap, DPAPI/ACL hardening, and validation; it is stacked on PR #3.
- PR #6: isolated Windows verifier and reliable failure/cleanup behaviour.
- PR #7: manifest parsing, resource scheduling, and raw command execution.

## What the AdventureText prototype implements

- Strict AI task-plan validation, dependency-cycle and overlap detection.
- GitHub repository privacy and runner inventory checks, planning briefs, human-approved Actions dispatch, run monitoring, and cancellation.
- Windows/Fedora setup-script prototypes, runner naming, and secret redaction.
- The physically proven isolated worktree, dual scope gate, independent test, validated-only commit, task-only push, draft-PR, report, and cleanup lifecycle.

## Non-duplication rules

- Extend ForgeGrid's coordinator and embedded dashboard; do not add a second Director web service.
- Extend ForgeGrid's worker; do not add a parallel permanent worker daemon.
- GitHub Actions is the authoritative queue for an AI coding attempt. ForgeGrid's LAN scheduler remains for builds, tests, exports, assets, servers, clients, packaging, offline work, and health jobs.
- Each attempt selects exactly one `DispatchBackend`: GitHub Actions now, or a future ForgeGrid LAN backend.
- Coordinator-selected arbitrary command strings are not permitted for security-sensitive execution. Workers execute immutable, locally approved profiles.
- Public ForgeGrid CI uses GitHub-hosted runners. The installer must refuse persistent coding-runner enrolment into a public repository.

## Open-PR reconciliation

PRs #3 and #4 remain a single stacked AgentBridge line and should be reviewed/landed independently of the GitHub Actions backend, then reconciled rather than reimplemented. PR #6 is narrowly scoped, green, and should be reused as the Windows verifier fix. PR #7 is not safe to merge as written; its disposition is recorded in `MIGRATION_FROM_ADVENTURETEXT_PROTOTYPE.md`.

No temporary AdventureText prototype distribution is a second supported product. It remains migration evidence only.
