# ForgeGrid Acceptance Matrix

This file separates **implemented behaviour**, **partially implemented behaviour**, and **planned acceptance criteria**. A feature being present in source code is not the same as having current physical-LAN evidence that the acceptance test passed.

Status meanings:

- **IMPLEMENTED** — current source contains the described mechanism.
- **PARTIAL** — some mechanism exists, but the older acceptance requirement is stronger than current behaviour.
- **NOT IMPLEMENTED** — current source does not implement the requirement.
- **HISTORICAL EVIDENCE** — a dated test record exists, but it does not prove every current build/revision.

## Core modes and pairing

### 1. Single executable — **PARTIAL against the old requirement**

Current binary supports:

- coordinator mode;
- worker mode;
- AgentBridge subcommands.

The previous requirement also said “or both”. Hybrid coordinator+worker mode is **not implemented**.

### 2. First-run wizard — **NOT IMPLEMENTED**

Current setup is CLI-driven. There is no current first-run UI wizard for mode/name/workspace selection.

### 3. Hardware detection — **IMPLEMENTED**

Workers collect and report:

- OS and OS version;
- architecture;
- CPU model;
- physical cores;
- logical processors;
- total/available RAM;
- free workspace disk.

### 4. Coordinator auto-discovery — **NOT IMPLEMENTED**

First-time workers require `-coordinator`. The current binary does not implement the older UDP/multicast auto-discovery requirement.

### 5. Pairing security — **IMPLEMENTED, with deployment caveats**

Current source implements:

- six-digit pairing codes;
- five-minute expiry;
- invalidation after successful pairing;
- failed-attempt counting/rate limiting;
- TLS fingerprint pinning for workers;
- per-worker bearer tokens for worker-originated APIs.

See `SECURITY.md`: the coordinator's operator/dashboard endpoints do not yet have a complete authentication layer.

## Project and job mechanics

### 6. Manifest parsing — **PARTIAL against the old requirement**

Implemented:

- strict YAML parsing with unknown-field rejection;
- required project name;
- at least one task;
- required execution profile;
- OS/RAM/core requirement fields;
- execution profile/args/env/timeout fields;
- artefact-pattern fields.

Current scheduler enforcement:

- `os` is enforced when choosing a worker;
- `min_ram_gb` is **not yet enforced**;
- `min_cores` is **not yet enforced**.

### 7. Mirror synchronisation — **NOT IMPLEMENTED**

There is no current differential file synchronisation path.

### 8. Godot headless build — **NOT IMPLEMENTED AS A FIRST-CLASS PROFILE**

Current execution profiles are `go`, `node`, `python` and internal `test`. There is no `godot` profile or built-in artefact-return pipeline.

### 9. Log streaming — **PARTIAL**

Workers capture stdout/stderr while the process runs, cap retained logs, and return them with job status/result updates.

The coordinator does not currently receive near-real-time WebSocket log streaming.

### 10. Cancellation and retry — **PARTIAL / NOT PROVEN END TO END**

Current code contains:

- coordinator-side `cancelled` state;
- worker-side context cancellation support.

However, worker job polling currently returns only pending jobs. A cancellation applied to an already-running job is therefore not a proven end-to-end cancellation notification mechanism.

There is no current general retry API matching the old acceptance criterion.

### 11. Worker resource constraints — **NOT IMPLEMENTED beyond OS selection**

Although manifests parse `min_ram_gb` and `min_cores`, the current Director chooses the first online worker satisfying the OS requirement. RAM/core scheduling is still roadmap work.

## Resilience and isolation

### 12. Workspace isolation — **PARTIAL**

The worker constrains the selected working directory to its configured ForgeGrid workspace and rejects a path that resolves outside it.

The spawned process still runs with the worker OS account's permissions. This does **not** prove that a Python/Node/Go job cannot open paths outside the workspace. The old directory-traversal acceptance requirement therefore is not satisfied by the current working-directory check alone.

### 13. Worker disconnect handling — **PARTIAL**

The coordinator marks an online worker offline after more than 15 seconds without a heartbeat.

The current coordinator does not automatically mark/retry/reassign an already-running job solely because its worker becomes offline.

### 14. Worker reconnect — **IMPLEMENTED; historical physical evidence exists**

Workers persist paired identity and reload it on later startup.

The historical `dist/ForgeGrid-USB/Documentation/REAL_LAN_TEST_RESULTS_2026-08-04.md` records a Windows worker reconnecting using saved identity after Fedora↔Windows pairing/heartbeat succeeded.

That record also states that coordinator-side completion output/exact hashes were not independently preserved, so it should not be broadened into proof that the complete current job path passed.

## Challenge test jobs — **IMPLEMENTED**

A coordinator test job contains a random challenge. The worker returns SHA-256(challenge), and the coordinator independently compares the expected value before recording success.

Historical 4 August 2026 physical-LAN evidence shows two challenge jobs starting on Windows, but does **not** preserve sufficient coordinator-side evidence to prove their final accepted results.

## Manifest execution — **IMPLEMENTED at basic level**

For an `execute` job, the worker:

1. resolves a named execution profile;
2. resolves the workspace working directory;
3. applies the requested/default timeout;
4. launches the process;
5. captures bounded stdout/stderr;
6. reports completion/failure/cancellation and exit-code information.

Artefact collection is not part of this current execution path.

## E2E integration-suite target — **NOT SATISFIED AS ORIGINALLY WRITTEN**

The older target required one coordinator, three simulated workers, five parallel jobs, deliberate failure/cancellation, and artefact collection assertions.

The repository has substantial Go unit/integration-style tests, but the exact acceptance target above includes features that are not currently implemented (notably artefact collection and proven running-job cancellation). Do not label that full target as passed until an actual test demonstrates it.

## Physical Windows/LAN evidence

Historical record: `dist/ForgeGrid-USB/Documentation/REAL_LAN_TEST_RESULTS_2026-08-04.md`.

What that record supports:

- Fedora and Windows used the recorded build SHA;
- pairing worked;
- heartbeat worked;
- the Windows worker reported hardware;
- saved-identity reconnect worked;
- two test jobs visibly started on Windows.

What it explicitly does **not** prove:

- final coordinator-side challenge verification for those jobs;
- multi-worker scaling;
- complex build workflows;
- a trustworthy pass from the then-current `VERIFY-WINDOWS.bat` (the record notes it printed success after an input-redirection error).

## Current acceptance priorities

The next useful acceptance milestones are:

1. enforce RAM/core constraints and test scheduling on mixed workers;
2. make running-job cancellation observable end to end;
3. add/rebuild a Windows verification script that fails closed;
4. preserve coordinator-side evidence for physical LAN test-job completion;
5. implement and test artefact collection before claiming artefact acceptance;
6. decide whether project synchronisation is Mirror mode, Git mode, or intentionally external to ForgeGrid;
7. add operator authentication before treating the coordinator as safe on an untrusted LAN.
