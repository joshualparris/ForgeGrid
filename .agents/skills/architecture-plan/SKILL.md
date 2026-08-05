---
name: architecture-plan
description: Requires a written implementation plan before material architectural, cross-cutting, security-boundary, storage-format, process, service, or interface changes.
---

# Architecture Plan

## Trigger

Create a plan before implementation when work:

- adds a server, process, executable, entry point, or scheduled task
- changes public APIs or CLI contracts
- changes storage or wire formats
- crosses authentication or trust boundaries
- introduces a dependency
- changes multi-platform architecture
- changes more than a small, tightly coupled set of files
- affects deployment, bootstrap, recovery, or agent orchestration

Trivial isolated fixes do not require a large design document.

## Plan contents

State:

1. **Problem**
   - current behaviour
   - required behaviour
   - measurable acceptance criteria

2. **Scope**
   - files expected to change
   - files explicitly excluded
   - new files or dependencies
   - branch and PR target

3. **Data and control flow**
   - caller and callee
   - process boundaries
   - trust boundaries
   - secret movement
   - persistence and restart behaviour

4. **Interfaces**
   - CLI flags
   - endpoints
   - request and response schemas
   - file formats
   - compatibility impact

5. **Failure behaviour**
   - timeouts
   - retries
   - rollback
   - cleanup
   - partial-write handling
   - crash and restart behaviour

6. **Security**
   - authentication
   - authorisation
   - sender identity
   - encryption
   - secret-at-rest protection
   - platform-specific permissions

7. **Testing**
   - red-green tests
   - integration tests
   - race tests
   - cross-builds
   - physical validation
   - independent review

8. **Migration and rollback**
   - compatibility with existing state
   - safe disable or revert path
   - whether credentials or state must rotate

## Approval boundary

For material plans, obtain approval before writing implementation code.

Do not interpret approval of the plan as approval to merge or deploy.

## Plan fidelity

During implementation:

- compare edits against the plan
- stop before material deviations
- update the plan or request approval for changed architecture
- do not solve a blocked design by creating an unplanned alternate service or entry point
