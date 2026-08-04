# Windows Verifier Documentation

## What the Verifier Checks
The `VERIFY-WINDOWS.bat` script performs a localized integration test on the Windows platform. It verifies:
1. The `ForgeGrid.exe` binary exists in the expected directory.
2. The coordinator component successfully starts and binds to the specified port.
3. The coordinator HTTP API is responsive and returns an HTTP 200 OK status code.
4. Clean shutdown works via `taskkill`.

## Physical Execution Requirement
This script **must be physically executed on a Windows machine**. While Linux (Fedora) can perform a static review of this script and its logic, it cannot properly execute or simulate the Windows `start`, `powershell`, or `taskkill` environments to validate runtime behaviour.

## Exit Codes
The script enforces strict process exit codes:
- **Success (Exit Code 0)**: Returned *only* if the entire initialization, HTTP polling, and teardown sequence completes flawlessly.
- **Failure (Non-Zero Exit Code)**: Returned immediately if any stage fails. The script will halt execution, print the exact stage that failed, and return exit code 1 to the caller, preventing false-positive success reports.
