# Acceptance Tests

## Core Requirements
1. **Single Executable**: Verify the Go binary can launch as Coordinator, Worker, or both.
2. **First-Run Wizard**: Verify successful prompting of node name, workspace, and mode selection.
3. **Hardware Detection**: Verify OS, CPU model, logical threads, RAM, and disk space are properly read and reported in both Windows and Linux.
4. **Auto-Discovery**: Verify a Worker automatically detects a Coordinator on the same LAN without manual IP entry.
5. **Pairing Security**: Verify a 6-digit code pairs a worker. Ensure mismatched codes reject pairing. Ensure the TLS fingerprint is visible.

## Project & Job Mechanics
6. **Manifest Parsing**: Verify `forgegrid.yaml` correctly triggers jobs, limits resources (e.g. `min_ram_gb`), and filters by OS.
7. **Mirror Sync**: Verify that modifying a file on Coordinator updates it on the Worker before the next job, without re-transmitting unchanged files.
8. **Godot Headless Build**: Verify a simple Godot headless export runs successfully and returns `build/**` artefacts.
9. **Log Streaming**: Ensure `stdout` and `stderr` from the job are visible in the Coordinator dashboard in near real-time.
10. **Cancellation & Retry**: Verify cancelling a running job terminates the child process cleanly. Verify retrying restarts it.
11. **Worker Constraints**: Ensure the "Very light worker" (Celeron N4500, 4GB RAM) does not get assigned heavy compilation jobs if `min_ram_gb: 8` is set.

## Resilience & Isolation
12. **Workspace Isolation**: Attempt directory traversal via job command (e.g., `cat ../../etc/passwd`) and ensure it is blocked/rejected.
13. **Worker Disconnect**: Disconnect a worker mid-job. Verify the Coordinator marks the job as failed or retries, and the dashboard reflects "Worker offline".
14. **Worker Reconnect**: Bring the worker back online. Ensure it reconnects seamlessly and is ready for new jobs.

## E2E Integration Suite
A Go test program must launch:
- 1 Coordinator (in-memory or tmp dir).
- 3 Simulated Workers.
- Inject 5 parallel jobs.
- Verify 1 fails as instructed.
- Verify 1 is cancelled successfully.
- Assert all artefacts are collected for the successful 3 jobs.
