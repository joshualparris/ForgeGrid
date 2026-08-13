# ForgeGrid Roadmap

ForgeGrid is evolving from a localized prototype into a fully autonomous, zero-maintenance "appliance" fleet for distributed CI/CD and AI agent workloads. The core hub-and-spoke orchestration is proven and stable.

The following outlines the immediate next steps to make managing and upgrading the runner fleet "super easy".

## 1. Over-the-Air (OTA) Worker Upgrades
**Goal:** Upgrade the entire fleet of runner binaries from the central dashboard without touching the physical laptops.
*   **Implement `SelfUpdate` Profile:** Add a new execution profile to the worker. 
*   **Artifact Distribution:** When a new `ForgeGrid.exe` is compiled, upload it to the coordinator's artifact store.
*   **Workflow:** Dispatch a broadcast manifest to all Windows runners. The manifest instructs the runners to download the new `.exe`, gracefully kill their current process, and swap the executable.
*   **Auto-Restart:** Rely on the Windows Service Manager (SCM) to instantly restart the runner with the newly dropped binary.

## 2. Environment Bootstrapping & Parity
**Goal:** Ensure workers always have the correct CLI tools required by their capabilities so jobs don't fail with "executable not found".
*   **Implement `BootstrapEnvironment` Profile:** A task profile that can automatically install or update dependencies.
*   **Package Management Integration:** Use `winget` or `chocolatey` on Windows runners to automatically install `go`, `node`, `godot`, and AI agents like `codex` or `antigravity`.
*   **PATH Management:** Ensure the worker process reloads its environment variables dynamically if a new tool is installed during a bootstrap job.

## 3. One-Click UI Job Templates
**Goal:** Move away from raw YAML manifest entry in the dashboard and provide standard, clickable playbooks.
*   **Template Library:** Add specific buttons to the web UI for common workflows:
    *   *Build & Export Godot Game (Windows)*
    *   *Run Go Test Suite*
    *   *Agent: Implement Feature (Codex/Antigravity)*
*   **Dynamic Forms:** Allow users to simply select a repository and branch from a dropdown, and the UI will generate and submit the correct manifest behind the scenes.

## 4. Enhanced Messaging & Telemetry
**Goal:** Improve visibility into exactly what the runners are doing, especially when executing autonomous agent tasks.
*   **Real-time Output:** Stream stdout/stderr from executing jobs back to the coordinator in real-time rather than waiting for the job to complete.
*   **Deep Agent Integration:** Standardize the AgentBridge payloads so that AI agents (like Antigravity) running on the workers can stream their thoughts and git commits directly into the ForgeGrid dashboard's messaging tab.
