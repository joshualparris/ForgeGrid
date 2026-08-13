# ForgeGrid

ForgeGrid is a portable local-network development cluster for distributing game-development jobs across several Windows and Linux computers. It acts as a lightweight standalone coordinator and worker system designed specifically to handle independent concurrent jobs like:

- Game builds and exports
- Automated tests
- Code linting and type checking
- Asset processing
- Dedicated game servers
- Automated multiplayer clients
- Optional AI coding-agent tasks
- Packaging and release creation

## Features

- **No External Dependencies**: ForgeGrid ships as a single compiled binary for Windows and Linux with zero external database dependencies.
- **Secure by Default**: All communications over the local network are encrypted using self-signed TLS certificates with Trust Pinning.
- **Hardware-Aware**: Automatically measures RAM, Workspace Disk, Logical/Physical Cores, OS, and Architecture to optimally allocate jobs.
- **Credential Persistence**: Identity tokens and hashes are securely stored natively (in `%LOCALAPPDATA%` on Windows, or `~/.local/share/` on Linux).
- **USB-Ready**: The `dist/ForgeGrid-USB` directory is designed to be fully copied onto a portable drive and carried to cluster nodes.

## Getting Started

ForgeGrid runs under two modes: `coordinator` and `worker`.

### 1. Coordinator Setup
Run this on your main machine (e.g. Linux workstation):
```bash
./forgegrid -mode coordinator -port 8080
```
Upon launching, it will display its **Local IP Address** and its **TLS Fingerprint**.

### 2. Worker Setup
On any node you want to attach (e.g. Windows laptop), generate a pairing code from the coordinator's web dashboard, then run:
```powershell
.\ForgeGrid.exe -mode worker -name "Worker-1" -coordinator 192.168.1.10 -code 123456 -fingerprint <FP>
```

Once paired, the worker securely stores its tokens. For subsequent launches, you only need to run:
```powershell
.\ForgeGrid.exe -mode worker
```

For Git-backed development jobs, each worker must explicitly allow the repositories it is willing to clone and run. Pushing is a second opt-in:
```powershell
.\ForgeGrid.exe -mode worker -allowed-repos "https://github.com/you/game.git,https://github.com/you/app.git" -allow-push
```

Linux workers can use the same flags, or environment variables:
```bash
FORGEGRID_ALLOWED_REPOS="https://github.com/you/game.git" FORGEGRID_ALLOW_PUSH=true ./forgegrid -mode worker
```

Workers can also advertise labels and capabilities for safer scheduling:
```bash
./forgegrid -mode worker -labels "trusted,linux-build" -capabilities "go,node,codex,godot,github-pr"
```

### 3. Dispatch Repo Jobs
Submit a manifest to the coordinator from the command node. Git jobs require a pinned `base_commit`, run in an isolated worktree, and can optionally commit/push only when both the manifest and worker policy allow it.

```yaml
project: "TextGame"
repository:
  url: "https://github.com/you/TextGame.git"
  base_commit: "0123456789abcdef0123456789abcdef01234567"
  branch: "forgegrid/improve-parser"
  create_pr: true
  pr_title: "Improve parser diagnostics"
  pr_body: "Automated ForgeGrid branch for review."
tasks:
  codex-improve-parser:
    requirements:
      os: "linux"
      min_ram_gb: 8
      labels: ["trusted"]
      capabilities: ["codex"]
    execution:
      profile: "CodexExec"
      parameters:
        prompt: "Improve parser errors, add tests, and keep changes scoped."
      timeout_seconds: 3600
      max_retries: 1
      changes:
        commit: true
        push: true
        commit_message: "Improve parser diagnostics"
```

Available execution profiles include `GoTest`, `GoBuild`, `NodeBuild`, `NodeTest`, `NodeLint`, `PythonUnittest`, `GodotExport`, `AIAgent`, and `CodexExec`.

For real multi-laptop validation, GitHub credentials, PR creation, rollback, and firewall troubleshooting, see [COMMAND_NODE_RUNBOOK.md](Documentation/COMMAND_NODE_RUNBOOK.md).

## Dashboard
The coordinator exposes a web UI on port `8080` (e.g., `https://127.0.0.1:8080`). From the dashboard, you can view worker heartbeats, hardware capabilities, job status, and generate new one-time pairing codes.

## Security Architecture
Please refer to [SECURITY.md](SECURITY.md) for detailed information on how ForgeGrid protects credentials, enforces TLS, prevents spoofing, and handles job cryptographic challenges.

## Additional Documentation
- [ARCHITECTURE.md](ARCHITECTURE.md) - System architecture and workflow details.
- [ACCEPTANCE_TESTS.md](ACCEPTANCE_TESTS.md) - Outlines end-to-end functionality guarantees.
- [RELEASE_NOTES.md](RELEASE_NOTES.md) - Changelog and specific version statuses.
