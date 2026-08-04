# Windows AgentBridge Pre-Check

## 1. Repository Status
- **Validation Branch:** `validation/windows-agentbridge-bootstrap`
- **Base Branch for PR:** `feature/secure-agent-bridge`
- **Commit SHA:** `e03d09c6da4ab584b164d3f04850b0b3c385474a`

## 2. Test Results
- **Ordinary Windows Tests:** **FAILED**
  - `TestMessageLifecycle` failed: "Windows should see 1 message, got 0"
  - `TestIntegrationConcurrent` failed: "Expected 700 messages, got 202"
- **Race Test (`go test -race`):** UNAVAILABLE on Windows due to the Go race build requiring CGO and a supported C toolchain. Fedora remains responsible for the Linux race run.
- **Build (`go build`):** **PASSED** (Exit Code 0)

## 3. Configuration Path Finding
- **Defect:** `internal/agentbridge/cli.go` incorrectly placed Windows credentials under `%USERPROFILE%\.config\forgegrid`.
- **Status:** **FIXED** on the validation branch. The code now respects `runtime.GOOS` and uses `%LOCALAPPDATA%\ForgeGrid\agentclient.json` for Windows, with a current-user-only ACL applied using `icacls`.
- **Token Storage Format:** Plaintext with filesystem protection (Current-User ACL).

## 4. Bootstrap Design
- **Cryptographic Construction:** Option B — RSA 3072 with RSA-OAEP-SHA256, plus Ed25519 digital signatures.
- **Implementation:** A Go utility (`.agents/scripts/bootstrap-crypto`) performs the crypto operations. The utility is wrapped by PowerShell scripts that manage Windows DPAPI (CurrentUser scope) protection for the private key, ACLs, and safe file cleanup.
- **Encryption & Authentication:** The token and registration material will be encrypted by the Fedora coordinator using the RSA public key and signed using an Ed25519 private key before transmission.
- **Fingerprint (SHA-256):** `2515679f04ca9711e7dd88cabff84f3c341b1e0d8a88fb82f4607d2bdd7b3419`

## 5. Polling and Scheduling Helper Status
- **Local Polling Helper:** Implemented at `.agents/scripts/agentbridge-poll.ps1`. Checks inbox, limits processing to one message, acknowledges before acting, and refuses to execute arbitrary shell instructions.
- **Task Scheduler script:** Implemented at `.agents/scripts/install-poll-task.ps1`. (Disabled by default).
- **Antigravity Scheduling Prompt:** Drafted in this report (see Section 7) for future use.

## 6. Remaining Blockers
- Awaiting Fedora to encrypt and transmit the registration bundle using the provided `bootstrap-public.pem`.
- One manual round trip must succeed before the scheduled polling task is enabled.

## 7. Antigravity Prompt for Scheduled Operation
When ready, run the following slash command to set up the recurrent agent task:

`/schedule "Open C:\dev\ForgeGrid. Read AGENTS.md and the inbox workflow. Run the 'ForgeGrid.exe agent-bridge inbox' command. Select no more than one pending instruction. Acknowledge it. Validate its scope and safety. Perform the allowed development or validation task. Return a result or failure through AgentBridge. Never push directly to main. Never execute arbitrary remote shell commands. Do nothing when the inbox is empty." cron="*/5 * * * *"`
