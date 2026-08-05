---
name: cross-platform-validation
description: Enforces Windows and Linux correctness for paths, permissions, build tags, process handling, persistence, scripts, and runtime claims in cross-platform projects.
---

# Cross-Platform Validation

## Platform matrix

Before changing shared code, identify:

- supported operating systems
- supported architectures
- code that is portable
- code requiring build-tagged implementations
- checks that can be cross-built
- checks requiring physical runtime validation

## Go source organisation

- Keep Windows-only imports in `//go:build windows` files.
- Keep Unix-only imports in `//go:build !windows` or narrower platform files.
- Provide explicit unsupported-platform stubs when a feature is unavailable.
- Build package directories with `go build .`, not an individual source file, when build-tagged companions are required.

## Paths and environment

- Do not hardcode repository paths in portable scripts.
- Windows batch files should resolve resources relative to `%~dp0` where appropriate.
- Handle spaces in paths.
- Use platform-correct user-data directories.
- Do not infer Windows merely from a path separator in logic where `runtime.GOOS` is clearer.

## Permissions

- POSIX `0600` and `0700` are not Windows ACL enforcement.
- Apply and verify Windows ACLs before writing secret bytes.
- Review inheritance, current-user SID, SYSTEM, Administrators, and unrelated-user access.
- Verify permissions after atomic rename.

## Process control

- Capture exact child PIDs.
- Never kill by image name when unrelated instances could exist.
- Verify readiness, not only launch success.
- Verify cleanup on success and failure.
- Distinguish forced termination from graceful shutdown.

## Persistence

Review:

- atomic replace semantics
- rename-over-existing behaviour
- file locking
- flush and close
- corruption handling
- restart recovery
- concurrent writers

Do not assume Unix persistence behaviour is identical on Windows.

## Shell and script correctness

For batch, PowerShell, and shell scripts:

- inspect quoting and delayed expansion
- preserve exact exit codes
- avoid unsupported input redirection
- avoid pipeline exit-code confusion
- bound polling loops
- test from a different working directory
- test missing executable, occupied port, early exit, and cleanup paths

## Required validation

Where applicable run:

```bash
go test -count=1 ./...
go test -count=1 -race ./...
go vet ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=linux GOARCH=amd64 go build ./...
```

A successful cross-build does not prove physical runtime behaviour. State physical validation as pending until performed.
