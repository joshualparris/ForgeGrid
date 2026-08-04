# ForgeGrid Architecture

## Overview
ForgeGrid is a portable, local-network development cluster designed to distribute game-development jobs (such as Godot builds, tests, and CI tasks) across multiple machines. It compiles into a single, standalone binary for Linux and Windows, without relying on external dependencies like Docker or Java.

## Core Components
1. **Single Binary Executable (Go)**:
   - **Coordinator Mode**: Manages the cluster, maintains state, schedules jobs, and serves the dashboard.
   - **Worker Mode**: Discovers and connects to the coordinator, executes assigned jobs, and streams logs/artefacts back.
   - **Hybrid Mode**: Runs as both Coordinator and Worker simultaneously.

2. **Embedded Dashboard**:
   - Built with Vanilla TypeScript/Preact.
   - Embedded directly into the Go binary using `go:embed`.
   - Opened automatically in the default browser when running as a Coordinator.

## Networking & Discovery
- **Discovery**: Workers auto-discover the Coordinator via UDP broadcast on the local LAN. Manual IP entry is also supported.
- **Communication**: All RPC, heartbeat, and log-streaming communication between the Coordinator and Workers is handled over TLS (HTTPS/WebSockets).

## Job Scheduling & Execution
- **Scheduler**: The Coordinator maintains a queue of jobs defined by the `forgegrid.yaml` manifest. It assigns jobs based on worker OS, RAM, CPU capability, and user-defined labels.
- **Execution Engine**: Workers execute commands in a sandboxed/restricted workspace. They stream `stdout` and `stderr` back to the Coordinator and upload matched artefacts upon completion.

## Project Synchronization
- **Mirror Mode**: The Coordinator synchronizes project files to workers using a differential synchronization protocol (comparing size, mtime, and SHA-256).
- **Git Mode**: Workers pull directly from a Git repository using designated branches/worktrees, isolating concurrent jobs.

## Extensibility
- An adapter-based plugin system for command execution, initially supporting Godot Engine commands.
- CLI-Agent adapter for AI coding agents to run tasks in isolated Git worktrees.
