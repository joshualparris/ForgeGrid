# Windows Verifier Documentation

## What the Verifier Checks
The `VERIFY-WINDOWS.bat` script performs an isolated, localized integration test on the Windows platform. It verifies:
1. The `ForgeGrid.exe` binary exists relative to the script itself (`%~dp0`).
2. An isolated test coordinator can start successfully. The exact Process ID (PID) is securely captured.
3. The coordinator HTTPS API initializes properly via a real TLS handshake (using `curl.exe -k -sS -f`), polling multiple times over 10 seconds for readiness.
4. Clean teardown works by strictly targeting the captured test PID for termination.

## Physical Execution Requirement
This script **must be physically executed on a Windows machine**. While Linux (Fedora) can perform a static review of this script and its logic, it cannot properly execute or simulate the Windows `Start-Process`, `powershell`, or `taskkill` environments to validate runtime behaviour.

## Exit Codes and Cleanup
The script enforces strict process exit codes:
- **Success (Exit Code 0)**: Returned *only* if the entire initialization, HTTPS polling, and specific PID termination sequence completes flawlessly.
- **Failure (Non-Zero Exit Code)**: Returned immediately if any stage fails.

Whether successful or failing, the script guarantees it stops *only* the process it created and cleans up temporary files. Note that forcing process termination via `taskkill` is used for test isolation, rather than a clean graceful shutdown. If cleanup fails to safely terminate the tracked PID or remove temporary files, the script also returns a non-zero exit code.
