# ForgeGrid / DadLAN Fleet — Status and Remaining Work

_Last updated: 2026-09-04, branch `forgegrid-consolidation`, HEAD `69a74f6616f0772e21d1c65b2781d3f6f653be2d`._

This document is the current, verified state of the DadLAN ForgeGrid rollout and
everything still left to do. Everything under "Current State" below was checked
against the live coordinator API or a real worker job — nothing here is
aspirational. Everything under "Remaining Work" is not done yet.

## Current State

**Coordinator:** running on AVANCE-WS7 (Fedora), `forgegrid -mode coordinator -port 8080`.

**Fleet:** 11/11 expected workers online (JParrisDesktop + Laptop01–10). Verified live via
`/api/workers` and, for every worker, a real dispatched-and-`COMPLETED` challenge job
through the coordinator (not Action1 output, not assumed):

| Worker | Arch | Commit running | Notes |
|---|---|---|---|
| JParrisDesktop | amd64 | `8ebf341621c8` | i5-9400, GTX 1660, git+node detected |
| Laptop01 | amd64 | `8ebf341621c8` | Ryzen 5 5600U, git/python/go/node detected |
| Laptop02 | amd64 | `a2a475978f28` | oldest build in the fleet — canary #2 target |
| Laptop03 | amd64 | `0640a220d246` | canary #1 target |
| Laptop04 | amd64 | `6255bca43cf3` | |
| Laptop05 | amd64 | `6255bca43cf3` | |
| Laptop06 | amd64 | `6255bca43cf3` | |
| Laptop07 | amd64 | `6255bca43cf3` | SMB to this host needs IP-based mount; mDNS resolution is unreliable (see Risks) |
| Laptop08 | amd64 | `6255bca43cf3` | |
| Laptop09 | amd64 | `6255bca43cf3` | very old hardware (Celeron T3500, 4GB RAM); works, but slow — do not aggressively re-touch it |
| Laptop10 | **386** | `6255bca43cf3` | 32-bit; proves architecture-specific artifact selection is required and working |

Aggregate fleet hardware (from real worker heartbeat data, not estimated):
**34 physical cores / 46 logical threads / ~99 GB RAM** across the 11 workers, plus
AVANCE-WS7 itself (i5-10500T, 6c/12t, 24GB, coordinator only, not a worker) for
**~40 cores / ~58 threads / ~123 GB** total. Storage capacity and GPU inventory across
the fleet have **not** been independently verified by this document's author and
should not be treated as authoritative until a real hardware census is run through
ForgeGrid itself.

**Two commits awaiting independent (Codex) review, both tagged `[PENDING CODEX REVIEW]`:**

- `8ebf341` — Windows-safe binary replacement (`safeReplace`, no more rename-over-existing),
  honest rollback (checks every filesystem/start error, reports `ROLLBACK_FAILED` instead of
  implying recovery), a duplicate-update guard (`tryBeginUpdate`/`endUpdate` keyed by update
  ID), a fix for a real bug in `file://` artifact-URL handling on Windows (confirmed broken,
  then fixed, then reverified against a live worker), and `GOOS=windows GOARCH=386` added to
  `build.sh` for Laptop10-class hardware.
- `69a74f6` — the regenerated `dist/ForgeGrid-USB` release bundle built from that exact
  `8ebf341` revision (built *after* committing the code, so the manifest's embedded commit
  is truthful, not stale). Windows amd64, Windows 386, and Linux amd64 artifacts; every
  hash independently re-verified with `sha256sum` against `update-manifest.json` and
  `CHECKSUMS.txt`.

The live coordinator already detects this manifest (`/api/updates/status` no longer reports
"no update manifest found"), and dashboard eligibility was checked for Laptop03 (selects the
amd64 artifact) and Laptop10 (selects the 386 artifact) **without changing either machine**.

**No self-update has been applied to any real worker yet.** Laptop02 and Laptop03 are
untouched pending the review below.

## Remaining Work, In Order

1. **Codex review of `8ebf341`/`69a74f6`.** A structured review prompt has been prepared
   covering: Windows-safe replacement, rollback honesty, duplicate-update prevention,
   `file://` path handling, amd64/386 selection and fail-closed behavior for unsupported
   architectures, and manifest/hash truthfulness. Verdict must be one of `APPROVED FOR
   LAPTOP03 CANARY`, `CHANGES REQUIRED`, or `BLOCKED`. **Nothing below happens until this
   comes back approved.**

2. **Laptop03 canary self-update**, `0640a220d246` → the approved build, through
   ForgeGrid's own update mechanism (never a manual binary swap unless the updater proves
   defective and the machine needs recovery). Must capture: pre-update identity/build,
   update ID, selected artifact + SHA, staged → applying → restarting → verifying →
   completed transitions, service/worker reconnect, fresh heartbeat, new build SHA, and a
   real post-update challenge job that reaches `COMPLETED`. If it fails, stop — do not move
   to Laptop02 — and if rollback fires, prove the old binary actually came back with its own
   fresh heartbeat.

3. **Laptop02 canary self-update**, `a2a475978f28` → the approved build, same evidence
   standard, only after Laptop03 succeeds cleanly.

4. **Known gap, not yet fixed:** `update.NeedsUpdate()` compares the manifest's semantic
   `version` string ("0.8.0") only, not the commit. Since every recent build has kept
   `FORGEGRID_VERSION=0.8.0`, the dashboard currently labels workers "current" even when
   their commit is stale. This does **not** block explicit queuing (`handleQueueWorkerUpdates`
   matches on artifact compatibility, not `NeedsUpdate`), so it isn't a blocker for the
   canaries above, but it makes the dashboard's status column misleading and should be
   fixed as a small follow-up (compare commit too, or bump version per real release).

5. **Worker capability audit.** Most newly onboarded workers currently show
   `No usable tools detected yet` on the dashboard even though several (Laptop01,
   JParrisDesktop) already have git/python/go/node natively. Determine whether this is
   stale capability detection, a PATH issue, a one-time refresh that hasn't run, or tools
   genuinely absent — before provisioning anything. Do not install toolchains
   indiscriminately; build a useful pool sized to what each machine can actually carry
   (low-end hardware like Laptop09/10 should stay light-duty).

6. **Only after 1–5 are done:** the smallest ForgeGrid extensions actually needed for a
   real multi-worker development workload — resource-aware job requests (CPU/RAM hints
   against what each worker already reports), per-task git worktree isolation so parallel
   workers never collide on the same files, a single integration lane so results merge
   through review rather than chaos, and a dependency-aware task queue. No speculative
   architecture beyond what the first real workload needs.

7. **First real workload: PartyAI**, via AgentCouncil + ForgeGrid, deliberately spread
   across most of the 11 workers (heavier machines building/compiling, lighter machines
   running simulations/regression tests in parallel) as the actual proof that this fleet
   is a working distributed development cluster and not just eleven laptops on a shelf.

## Explicitly Not Doing (Yet)

- No bulk "Update All" — canaries prove the path one machine at a time.
- No distributed-development architecture built ahead of a real workload needing it.
- No further work on Laptop09 beyond keeping it online — it works, it's just old and slow.
- No new permanent artifact-serving route on the coordinator, authenticated or not, unless
  a real need is proven and it's reviewed (see Security below).
- No touching Laptop09 beyond what's already done, and no chasing a full hardware/GPU/storage
  census, unless it's actually needed for a workload decision.

## Security Notes Worth Preserving

- Earlier in this rollout, a temporary, unauthenticated `/api/artifacts/` route was added to
  the coordinator that served the entire ForgeGrid project directory over plain HTTP,
  including the coordinator's own TLS private key. It was killed and reverted the moment it
  was found. **Do not recreate anything like it.** Where a worker genuinely needs a file it
  can't reach any other way, the pattern used successfully in this rollout is a short-lived,
  narrowly-scoped local HTTP server serving only the specific file(s) needed, torn down
  immediately after the transfer completes — never a standing, directory-wide route.
- Action1 credentials are never persisted to disk by design (see
  `action1/fedora/action1_client.py` — "DadLAN does not persist the Client Secret"). Don't
  try to "fix" that; ask for them fresh each session.

## Risks / Things to Watch

- **mDNS-based SMB hostname resolution on AVANCE-WS7 is unreliable.** `/etc/fstab` entries
  for the DadLAN mounts use static IPs (not hostnames), and at least one entry (Laptop07)
  was found pointing at a stale IP. If a laptop's DHCP lease changes, its SMB mount can
  silently start failing with `cifs_mount failed w/return code = -113` even though the
  machine is genuinely reachable — check the real IP via Action1 before assuming SMB itself
  is broken.
- **Avance has two WiFi networks that are not mutually routable:** "Avance Business
  Technology" (where the coordinator lives) and "Avance Guest" (isolated from it). A laptop
  on Guest WiFi cannot reach the coordinator at all, which looks like a ForgeGrid pairing
  bug but isn't — check which network a misbehaving machine is on before debugging code.
- Laptop01 previously carried a stale worker registration from an old, pre-fix binary that
  had paired but wasn't reporting properly; it's been cleanly reinstalled, but if other
  machines show similar stale entries, check for old scheduled tasks or leftover installs
  before assuming the coordinator is wrong.
