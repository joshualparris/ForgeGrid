# Antigravity AgentBridge

AgentBridge provides a secure, HTTPS-pinned local network relay for orchestrating Antigravity agents across physical machines. 

## Registration
First, the coordinator creates an agent identity:
```bash
./forgegrid agent-bridge register --name fedora-orchestrator
./forgegrid agent-bridge register --name windows-test
```
This produces a one-time Authentication Token.

## Server
Start the relay server on the coordinator:
```bash
./forgegrid agent-bridge serve --port 9090
```
This uses ForgeGrid's pinned TLS certificates.

## Windows Client Setup
Save the token to an environment variable:
```powershell
$env:AGENT_NAME="windows-test"
$env:AGENT_TOKEN="<token>"
```

## Sending Messages
```bash
./forgegrid agent-bridge send --to windows-test --task my-task --type instruction --message "Hello Windows!"
```

## Polling and Responding
Agents read their inbox and acknowledge/complete messages:
```bash
./forgegrid agent-bridge inbox
./forgegrid agent-bridge ack --message-id <id>
./forgegrid agent-bridge complete --message-id <id> --result-file output.json
```

## Security
- No command execution support is built into AgentBridge itself.
- All tasks must be validated by the receiving Antigravity agent.
- Tokens are never exposed in responses or Git.
