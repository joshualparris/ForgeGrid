# Antigravity AgentBridge

AgentBridge is ForgeGrid's separate authenticated HTTPS message relay for coordinating external coding agents across machines. It moves structured instructions/results; **it does not itself execute arbitrary remote shell commands**.

## Commands currently available

```text
serve
rotate-tls
register
configure-client
reset-client
send
inbox
ack
complete
fail
```

## 1. Start the relay

On the coordinator/relay machine:

```bash
./forgegrid agent-bridge serve --port 9090
```

TLS is enabled by default. On first start AgentBridge generates its own certificate/key in its data store and prints the TLS fingerprint.

`--insecure` disables TLS and is intended only for isolated development/testing.

AgentBridge TLS/client state is separate from the normal ForgeGrid coordinator/worker pairing state.

## 2. Register agent identities

Run registration against the same AgentBridge store used by the relay:

```bash
./forgegrid agent-bridge register --name fedora-orchestrator
./forgegrid agent-bridge register --name windows-test
```

Each registration prints a random authentication token **once**. Save the token securely; the store retains its hash rather than displaying the plaintext token later.

## 3. Configure a client

The preferred current setup is `configure-client`, which stores the relay URL, agent name, token and pinned TLS fingerprint for later commands.

### Token through stdin

```powershell
Get-Content .\agent-token.txt -Raw | .\ForgeGrid.exe agent-bridge configure-client `
  -name windows-test `
  -url https://192.168.1.10:9090 `
  -fingerprint <FP> `
  -token-stdin
```

### Token through a file

```powershell
.\ForgeGrid.exe agent-bridge configure-client `
  -name windows-test `
  -url https://192.168.1.10:9090 `
  -fingerprint <FP> `
  -token-file .\agent-token.txt
```

When `-token-file` is used, ForgeGrid deletes that token file after reading it.

Client configuration paths:

- Windows: `%LOCALAPPDATA%\ForgeGrid\agentclient.json`
- Linux: `~/.config/forgegrid/agentclient.json`

The token remains plaintext inside the local client configuration. The Windows writer applies a current-user ACL; local account/filesystem security is therefore part of the trust boundary.

To remove the saved client configuration:

```powershell
.\ForgeGrid.exe agent-bridge reset-client
```

## Environment-variable fallback

Client commands can also obtain identity/token/fingerprint from:

- `AGENT_NAME`
- `AGENT_TOKEN`
- `AGENT_FINGERPRINT`

and can take URL/name/fingerprint flags directly.

For repeat use, `configure-client` is less error-prone because TLS fingerprint configuration is saved with the identity.

## 4. Send a message

```bash
./forgegrid agent-bridge send \
  --to windows-test \
  --task my-task \
  --type instruction \
  --message "Review the current branch and report the test result."
```

AgentBridge supports idempotency keys for callers that need duplicate-send protection.

## 5. Read and acknowledge the inbox

```bash
./forgegrid agent-bridge inbox
./forgegrid agent-bridge ack --message-id <id>
```

A polling/orchestration layer should:

1. read a bounded number of messages;
2. validate the sender/task/instruction;
3. acknowledge only the message it is taking responsibility for;
4. apply its own local safety/scope rules before doing work.

## 6. Return completion or failure

```bash
./forgegrid agent-bridge complete --message-id <id> --result-file output.json
```

or:

```bash
./forgegrid agent-bridge fail --message-id <id> --message "Validation failed"
```

## Trust boundary

AgentBridge provides:

- HTTPS transport by default;
- pinned certificate verification for configured clients;
- per-agent token authentication;
- structured message lifecycle/state;
- a relay that does not itself expose arbitrary shell execution.

AgentBridge does **not** make a received instruction safe merely because it was authenticated.

The receiving Antigravity/Claude/Codex automation remains responsible for:

- deciding whether the requested action is in scope;
- refusing dangerous or unexpected instructions;
- protecting local credentials/files;
- verifying work before returning success;
- avoiding direct pushes/changes that violate the repository workflow.

## Windows bootstrap material

The repository includes Windows bootstrap/polling helpers under `.agents/scripts` and a historical pre-check in `WINDOWS_AGENT_BRIDGE_PRECHECK.md`.

That pre-check records a particular validation branch/commit and unresolved blockers at the time it was written. Treat it as historical validation evidence, **not** as the current installation guide or a claim that every bootstrap step is presently validated.

## Security reminders

- Do not commit AgentBridge tokens.
- Verify the relay TLS fingerprint through a trusted channel before configuring a client.
- Rotate/reset credentials after suspected exposure.
- Do not use `--insecure` on an untrusted network.
- Keep the relay behind the host firewall on networks you control.
- Do not turn inbox messages directly into shell commands without a separate validation/execution policy.
