# Antigravity Workflow: Send Agent Result

**Trigger**: Called after executing an agent-bridge task.

## Steps

1. **Format Result**: Create a structured JSON result detailing the task execution outcome.
2. **Save locally**: Save it to a temporary file `result.json`.
3. **Submit**:
   - If successful: `forgegrid agent-bridge complete --message-id <id> --result-file result.json`
   - If failed: `forgegrid agent-bridge fail --message-id <id> --result-file result.json` (Note: you must use the underlying HTTP API or a script for explicit `fail` action if the CLI `complete` doesn't support the `fail` endpoint directly, but standard workflows can just mark complete with a failed status payload).
4. **Cleanup**: Delete `result.json`.
