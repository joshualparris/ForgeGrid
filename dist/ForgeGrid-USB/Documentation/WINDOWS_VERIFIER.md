# Windows Verifier Documentation

## What the Verifier Checks
The `VERIFY-WINDOWS.bat` and `VERIFY-WINDOWS.ps1` scripts perform an isolated, localized integration test on the Windows platform. It verifies:
1. The `ForgeGrid.exe` binary exists relative to the script itself (`%~dp0`).
2. An isolated test coordinator can start successfully. The **exact process identity** (PID and executable path) is securely captured.
3. A **unique coordinator identity** is pre-seeded for each test run to ensure strict validation.
4. The coordinator HTTPS API initializes properly, and the **HTTPS response** correctly returns the exact, unique identity expected for that test run.
5. **Occupied-port false-positive prevention**: It explicitly verifies that the HTTPS response identity matches its own spawned process, rather than mistakenly passing if an unrelated server happens to be occupying the test port.
6. **Cleanup** works correctly by strictly targeting the captured test PID for termination and removing all temporary isolated test directories.

## Physical Execution Requirement
This script **must be physically executed on a Windows machine**. While Linux (Fedora) can perform a static review of this script and its logic, it cannot properly execute or simulate the Windows `Start-Process`, `Invoke-RestMethod`, or process management environments to validate runtime behaviour.

## Exit Codes and Cleanup
The script enforces strict process exit codes:
- **Success (Exit Code 0)**: Returned *only* if the entire initialization, unique identity verification, and specific PID termination sequence completes flawlessly.
- **Failure (Non-Zero Exit Code)**: Returned immediately if any stage fails.

Whether successful or failing, the script guarantees it stops *only* the process it created and cleans up temporary directories. Forced process termination (`Stop-Process -Force`) is used for test isolation; it is not a graceful shutdown. If cleanup fails to safely terminate the tracked PID or remove temporary files, the script also returns a non-zero exit code.
