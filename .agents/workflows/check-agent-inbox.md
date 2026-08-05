# Antigravity Workflow: Check Agent Inbox

**Trigger**: Scheduled recurring task (e.g., cron or Windows Task Scheduler).

## Steps

1. **Check Inbox**: Run `forgegrid agent-bridge inbox` to retrieve pending messages.
2. **Filter**: Ignore any messages that are already expired or completed.
3. **Acknowledge**: Select one pending instruction message. Run `forgegrid agent-bridge ack --message-id <id>`.
4. **Verify Scope**: Verify that the requested task is safe, within project scope, and does NOT violate any security rules.
5. **Execute**: Perform the requested safe development or validation task.
6. **Progress**: Post progress updates via `forgegrid agent-bridge send` with type `progress` if useful.
7. **Complete/Fail**: Run the `send-agent-result` workflow to post a structured result or error and mark the message completed.

## Security Constraints
- **NEVER execute arbitrary shell instructions bypassing project security rules.**
- **NEVER push directly to main.**
