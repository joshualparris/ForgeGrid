# ForgeGrid / DadLAN Fleet — Status and Remaining Work

_Last updated: 2026-09-04, branch `forgegrid-consolidation`, HEAD `a93fc3600d3c` (code) /
verify against `git log -1` for the true current HEAD — this file is updated at each
milestone, not on every commit._

This document is the current, verified state of the DadLAN ForgeGrid rollout and
everything still left to do. Everything under "Current State" was checked against the
live coordinator API, a real worker job, or a real cross-compile/test run — nothing
here is aspirational. Everything under "Remaining Work" is not done yet.

## Current State

**Coordinator:** running on AVANCE-WS7 (Fedora), `forgegrid -mode coordinator -port 8080`,
rebuilt and restarted from the current HEAD (all 11 workers reconnected automatically,
no reinstall needed — worker credentials persist across coordinator restarts).

**Fleet:** 11/11 expected workers online (JParrisDesktop + Laptop01–10), each verified with
a real, `COMPLETED`, coordinator-issued challenge job (not Action1 output, not assumed).

**Two commits still awaiting independent (Codex) review, both tagged `[PENDING CODEX REVIEW]`,
untouched since they were written — `8ebf341` and `69a74f6`.** No self-update has been
applied to any real worker. Laptop02 and Laptop03 remain untouched pending that review;
this boundary has been respected throughout everything below.

### Hardware + capability census (real data, pulled live from `/api/workers`)

| Worker | Cores/Threads | RAM (total/avail) | Arch | Capabilities (verified) | Tier |
|---|---|---|---|---|---|
| Laptop01 | 6c/12t | 16.4GB / ~4-12GB | amd64 | git, python, go, node | **HEAVY** |
| JParrisDesktop | 6c/6t | 17.1GB / ~12GB | amd64 | git, node | **HEAVY** |
| Laptop02 | 2c/4t | 8.5GB | amd64 | git, python, go, node | MEDIUM (capability-rich) |
| Laptop04 | 2c/4t | 8.5GB | amd64 | none detected | MEDIUM |
| Laptop07 | 4c/4t | 8.5GB | amd64 | none detected | MEDIUM |
| Laptop03 | 4c/4t | 8.0GB | amd64 | none detected | MEDIUM |
| Laptop05 | 2c/4t | 8.4GB | amd64 | none detected | MEDIUM |
| Laptop06 | 2c/2t | 8.4GB | amd64 | none detected | LIGHT |
| Laptop08 | 2c/2t | 8.0GB | amd64 | none detected | LIGHT |
| Laptop09 | 2c/2t | 4.2GB | amd64 | none detected | LEGACY/LOW-SPEC |
| Laptop10 | 2c/2t | 3.2GB | **386** | none detected | LEGACY/LOW-SPEC |

Tiering is derived from `logical_threads*10 + total_ram_gb` (the same shape of score the
director's scheduler already uses), not from machine names.

**Capability detection was investigated, not assumed, and found honest.** Probed Laptop03
(zero capabilities) and JParrisDesktop (partial: git+node, not python/go) directly via
Action1 running as the same `LocalSystem` account the ForgeGrid worker service runs as: in
both cases the machine-wide `PATH` genuinely contained (or didn't contain) exactly what
ForgeGrid reported. **This is not a ForgeGrid detection bug.** Most DadLAN laptops simply
don't have dev tools installed system-wide; Windows services never see per-user `PATH`
regardless. `DetectCapabilities()`/`ValidateCapabilities()` re-run every heartbeat (~5s), so
nothing here is stale either. One regression test added locking in the (already-correct)
"no allowlist configured → report everything detected" behavior every real worker relies on.

**Only 2 of 11 workers can currently run Python at all** (Laptop01, Laptop02) — relevant to
the PartyAI proposal below.

### Aggregate fleet hardware

**34 physical cores / 46 logical threads / ~99 GB RAM** across the 11 workers, plus
AVANCE-WS7 itself (i5-10500T, 6c/12t, 24GB, coordinator only, not a worker) for
**~40 cores / ~58 threads / ~123 GB** total. Storage capacity and GPU inventory across the
fleet have **not** been independently verified through ForgeGrid itself and should not be
treated as authoritative.

### Resource-aware scheduling — already existed, now architecture-aware

`internal/director` was already a real, working scheduler (`SelectWorker`/`workerEligible`)
supporting min CPU threads, min available RAM, OS, labels, and capabilities, all against
live heartbeat data, failing closed (no job created) when nothing qualifies. It was missing
architecture. Added `Requirements.Architecture` and wired it through eligibility exactly
like OS. **Proven live on the real fleet, not just in tests:**

| Proof | Requirement | Job ID | Selected worker | Correctly excluded |
|---|---|---|---|---|
| Architecture | `architecture: amd64` | `job-c60e4a05e766522670c10ec931668445` | Laptop01 | Laptop10 (386) |
| RAM | `min_ram_gb: 8` | `job-6b884b96752234bede2f552115fe9e68` | JParrisDesktop (17GB) | Laptop09 (4GB), Laptop10 (3GB) |
| Capability | `capabilities: [go]` | `job-092d42c41c1f7a4d11a61126d130ea19` | Laptop01 | the 9 workers without `go` |
| Unsupported | `architecture: arm64` | *(none created)* | — | HTTP 503, `no eligible online worker found`, per-worker reasons listed |
| No requirements | *(none)* | `job-e05d796a6dc2cbc9220fc0abe4906834` | Laptop01 (highest score) | — |

The architecture/RAM/capability proof jobs all legitimately `FAILED` at execution (no
`go.mod` in an empty workspace — `GoBuild` with no repository configured), which is expected
and still valid evidence: the failure logs show `go` genuinely ran (`go: go.mod file not
found...`), proving both correct worker selection **and** that the reported capability was
real, not just correct scheduling in isolation.

**Known current limitation, not fixed (out of scope for this round):** the scheduler always
picks the *highest*-scoring eligible worker. There is no way yet to request "prefer a light
machine for light work" — only minimum thresholds. The unconstrained proof job above landed
on Laptop01 (a heavy machine) for exactly this reason. Worth knowing before the PartyAI
low-spec-compatibility lane is built.

Added `GitVersion`/`PythonVersion`/`GoVersion`/`NodeVersion` execution profiles (trivial,
read-only, no arguments) so a reported capability can be proven with a real job in the
future — **not yet exercised on the live fleet**, because doing so would require pushing a
new binary to a worker, which crosses the "no re-onboarding / no bulk-update" boundary this
session was told to hold. They'll prove themselves the next time a worker legitimately gets
a binary update (e.g., after the Laptop03/Laptop02 canaries).

### `NeedsUpdate()` truthfulness — fixed and proven live

Previously compared only the semantic version string; since several commits in a row have
all shipped as "0.8.0", the dashboard called stale-commit workers "current". Now compares
version **and** commit, failing conservatively (→ needs update) when either commit is
unknown. **Proven live:** after rebuilding and restarting the coordinator, `/api/updates/status`
now correctly reports Laptop02, Laptop03, and Laptop10 as `available` with the honest reason
*"is on 0.8.0 at an older commit"* — previously all three were wrongly labelled `current`.
This does not change queuing behavior (`handleQueueWorkerUpdates` already matched on artifact
compatibility, not `NeedsUpdate`), so it never blocked the canaries — only the dashboard label
was wrong, and that's now fixed.

## Remaining Work, In Order

1. **Codex review of `8ebf341`/`69a74f6`.** Unchanged from before — still the gate. Verdict
   must be one of `APPROVED FOR LAPTOP03 CANARY`, `CHANGES REQUIRED`, or `BLOCKED`.
   **Nothing about Laptop03/Laptop02 happens until this comes back approved.**

2. **Laptop03 canary self-update**, `0640a220d246` → the approved build, through ForgeGrid's
   own update mechanism only. Full lifecycle evidence required (see prior version of this
   doc / the original task brief for the exact checklist — unchanged).

3. **Laptop02 canary self-update**, `a2a475978f28` → the approved build, only after Laptop03
   succeeds cleanly.

4. **PartyAI first fleet run** (concrete proposal, not yet executed — see below).

5. **Scheduler follow-up (small, only if PartyAI actually needs it):** a way to express "this
   task is fine on a light machine" so unconstrained/small jobs don't default to the
   heaviest available worker. Only build this if the PartyAI run in step 4 actually shows it
   matters — don't build it speculatively.

## PartyAI — Concrete First Fleet Run Proposal

Inspected `/home/josh/dev/PartyAI-ForgeGrid` directly (not from memory/assumption). Current
real contents: a single-file Python prototype (`src/party_ai/game.py`, a small seeded
turn-based simulation), one test file (`tests/test_game.py`, 2 unit tests), a README that
*proposes* an 11-lane worker split (combat/AI/map-gen/inventory/etc.) but **no code is
actually split into those lanes yet** — that's a plan in prose, not existing structure. No
`pyproject.toml`/`setup.py` exists yet, and the README's documented `python -m unittest`
command **does not currently work as written** (0 tests discovered — needs `src` on
`PYTHONPATH` and `-s tests`, or a proper package install). This project appears to be under
active, very recent construction (all files dated today).

Given that reality, and that **only Laptop01 and Laptop02 have Python today**, the honest
first fleet run is much smaller than "11 workers, 11 game systems." Proposed concretely:

- **Lane 1 — Unit tests:** `python -m unittest` (fixed to actually discover tests) on
  Laptop01 or Laptop02, whichever is idle. Proves the existing 2 tests pass through a real
  ForgeGrid job.
- **Lane 2 — Deterministic simulation batch:** dispatch many `GameState(seed=N)` playthroughs
  across different seeds, split between Laptop01 and Laptop02 (the only Python-capable
  workers) — the natural "many independent parallel runs" workload this tiny prototype
  already supports today, via `simulate_turn()`'s existing determinism guarantee.
  This is genuinely the *only* PartyAI-specific work more than 1 worker can do right now.
- **Lane 3 — Fleet liveness/integration:** the other 9 workers can't run PartyAI's Python
  code yet. Their honest first-run contribution is what they can already do: a real
  ForgeGrid challenge job (or, once redeployed, the new `GitVersion`-style profiles) as
  a liveness/capability check, not game work.

**The real blocker to the bigger 11-worker vision isn't ForgeGrid — it's that 9 of 11
workers have no Python.** Before building anything bigger, this needs an explicit decision
(not an autonomous one): provision Python on some subset of the fleet (which ones, and is
it worth it on Laptop09/10-class hardware), or grow PartyAI's task set to include
tool-agnostic work (text/data analysis, hashing, file-based checks) that doesn't need
Python, so the low-spec/no-toolchain machines have real, honest work too.

## Explicitly Not Doing (Yet)

- No bulk "Update All" — canaries prove the path one machine at a time.
- No distributed-development architecture built ahead of a real workload needing it.
- No further work on Laptop09 beyond keeping it online.
- No new permanent artifact-serving route on the coordinator, authenticated or not.
- No provisioning Python/Go/Node on any additional machine without an explicit decision —
  see the PartyAI blocker above.
- No PartyAI feature development — inspection and proposal only, per this round's scope.

## Security Notes Worth Preserving

- Earlier in this rollout, a temporary, unauthenticated `/api/artifacts/` route was added to
  the coordinator that served the entire ForgeGrid project directory over plain HTTP,
  including the coordinator's own TLS private key. It was killed and reverted the moment it
  was found. **Do not recreate anything like it.** Where a worker genuinely needs a file it
  can't reach any other way, the pattern used successfully in this rollout is a short-lived,
  narrowly-scoped local HTTP server serving only the specific file(s) needed, torn down
  immediately after the transfer completes — never a standing, directory-wide route.
- Action1 credentials are never persisted to disk by design. Don't try to "fix" that; ask
  for them fresh each session.

## Risks / Things to Watch

- **mDNS-based SMB hostname resolution on AVANCE-WS7 is unreliable.** `/etc/fstab` entries
  for the DadLAN mounts use static IPs, and stale-IP failures have been seen (Laptop03,
  Laptop07). Check the real IP via Action1 before assuming SMB itself is broken.
- **Avance has two WiFi networks that are not mutually routable:** "Avance Business
  Technology" (coordinator) and "Avance Guest" (isolated). A laptop on Guest WiFi cannot
  reach the coordinator at all — check which network a misbehaving machine is on before
  debugging code.
- Coordinator restarts are safe and don't require touching any worker — credentials persist
  in the store and workers reconnect within a few heartbeat cycles automatically. Confirmed
  live this round (all 11 reconnected within ~10s of a coordinator restart).
