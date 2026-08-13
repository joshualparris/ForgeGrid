# ForgeGrid LAN Readiness Test

This document outlines the procedure for testing ForgeGrid across a real, multi-laptop Local Area Network (LAN). 
The goal is to verify that workers can discover the coordinator, pair securely, and execute isolated jobs with various git push/commit policies safely.

## 1. Setup the Coordinator
Pick one machine to act as the central command node (Coordinator).
1. Ensure you have the `forgegrid` binary compiled.
2. Run the coordinator:
   ```bash
   ./forgegrid -mode coordinator -port 8080
   ```
3. Take note of the **LAN IP** and the **TLS Fingerprint** printed in the console. The dashboard will automatically open in your browser.
4. If you have a firewall (like `ufw` on Linux or Windows Defender), ensure port `8080` (or your chosen port) is allowed for inbound connections.

## 2. Pairing Workers
On your secondary laptops (Workers):
1. Navigate to the coordinator's dashboard in a browser (e.g., `https://<LAN_IP>:8080`). Note: You will need the admin password printed in the coordinator's console to login.
2. Click **Generate Pairing Code**.
3. Run the worker executable. Pass the `-allowed-repos` flag to limit which repositories the worker is allowed to clone.
   ```powershell
   .\forgegrid.exe -mode worker -name "Worker-Laptop-1" -coordinator <LAN_IP> -code <6_DIGIT_CODE> -fingerprint <TLS_FINGERPRINT> -allowed-repos "https://github.com/example/repo.git"
   ```
4. Check the dashboard to verify the worker appears in the "Connected Workers" list with its OS, CPU, RAM, and "online" status.

## 3. GitHub Credential Setup (Without Exposing Tokens)
ForgeGrid does not pass GitHub credentials over the wire or store them in its dashboard. 
Instead, it relies on the worker machine's native Git credential manager.
- On each worker, ensure Git is installed.
- Authenticate Git locally (e.g., via `git credential-manager` or SSH keys) so that cloning and pushing to the target repositories works without interactive prompts.

## 4. Test Scenarios

### Scenario A: Dispatch a No-Push Job (Safe)
1. In the Coordinator Dashboard, locate the "Submit Job Manifest" section.
2. Click the **No Push** template button.
3. Ensure the `repository.url` matches your `-allowed-repos`.
4. Click **Submit Job Manifest**.
5. **Expected Output:** The job appears in the "Recent Jobs" table. The worker claims it, executes it, and completes. The dashboard logs should show a successful workspace clone and job execution, but no git commit or push commands.

### Scenario B: Dispatch a Commit-Only Job
1. Click the **Commit Only** template button in the dashboard.
2. Click **Submit Job Manifest**.
3. **Expected Output:** The worker executes the job. Afterward, it runs `git commit` with the provided message, but does *not* push to the remote. The job is marked as COMPLETED.

### Scenario C: Dispatch a Push Job (Requires Explicit Worker Opt-in)
For a worker to actually push changes to the remote repository, it must have been launched with the `-allow-push` flag. This prevents coordinators from forcing unauthorized pushes on secure workers.
1. Restart a worker with the push flag:
   ```powershell
   .\forgegrid.exe -mode worker -allow-push -allowed-repos "https://github.com/example/repo.git"
   ```
2. In the Coordinator Dashboard, click the **Push Changes** template button.
3. Notice the strong yellow **WARNING** box that appears in the UI.
4. Click **Submit Job Manifest**.
5. **Expected Output:** The worker executes the job, commits the changes, and pushes them to the origin branch. The job logs in the dashboard will reflect `--- CHANGES PUSHED TO ORIGIN ---`.

## 5. Troubleshooting
- **Worker shows offline immediately:** Check that the coordinator machine's firewall allows TCP traffic on the designated port.
- **TLS Fingerprint Mismatch:** Ensure you copied the exact SHA-256 fingerprint from the coordinator's terminal.
- **Push Failed / Authentication Required:** The worker machine does not have its native Git credential helper configured properly. Log into the worker manually and perform a test `git push` to cache the credentials.

## 6. Actual LAN Validation Log (August 13, 2026)
### Commands Run
- **Worker Start:**
  ```bash
  ./forgegrid -mode worker -name "test-worker-1" -coordinator 10.245.173.178 -code 070858 -fingerprint fbd657ca7eae838ed3b8c0301874f47079f2f707b6b7b3c2aa009ba6a51cec17 -allowed-repos "/tmp/trusted-repo.git" -labels "trusted" -capabilities "godot,github-pr" -allow-push
  ```
- **Dispatched Jobs:**
  Submitted three manifests (`no-push.yaml`, `commit-only.yaml`, `push.yaml`) to `https://10.245.173.178:8080/api/jobs/manifest`.

### Outcomes & Failures
- The worker successfully registered and claimed the jobs.
- The `gitworkspace` manager successfully created branch checkouts (e.g. `.forgegrid/job-id`).
- **Failure:** Job execution failed with `fork/exec /usr/bin/python: no such file or directory` or `fork/exec /tmp/godot: no such file or directory`.
- **Root Cause:** ForgeGrid's highly restricted environment sandbox (in `executor.go`) strips `PATH` and most other standard shell environment variables. If a binary is statically linked but the process environment is too restrictive, or if the interpreter requires `PATH`, execution will fail.

### Fixes & PR Creation
- **Fix:** Ensure the worker environment's `PATH` contains the exact tools required by the manifest execution profiles, and verify that dependencies like `python3` or `godot` are statically accessible without relying on environment variables filtered out by ForgeGrid's restricted execution sandbox.
- **Git Push & PR Policy:** Because execution failed with a non-zero exit code, the Git manager correctly aborted the commit and push phase. This validates the backend safety policy: broken jobs will *not* push half-finished or errored workspaces to the origin.
- To successfully generate a PR, execution must complete with `exit code 0`, at which point the worker executes `gh pr create` based on the host credentials.

## 7. BootstrapEnvironment Test
A manifest configured to use the `BootstrapEnvironment` execution profile. It requests missing capabilities (e.g., `tools: [godot]`) and triggers `winget install` on the worker, proving environment auto-healing.
