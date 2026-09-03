# ForgeGrid Roadmap

ForgeGrid is evolving from a localized prototype into a fully autonomous, zero-maintenance "appliance" fleet for distributed CI/CD and AI agent workloads. The core hub-and-spoke orchestration is proven and stable.

## 1. Over-the-Air (OTA) Worker Upgrades (IMPLEMENTED)
**Status:** IMPLEMENTED and TESTED ON FEDORA (Local/Network).
**Goal:** Upgrade the entire fleet of runner binaries from the central dashboard without touching the physical laptops.
*   **Implement `SelfUpdate` Profile:** Done.
*   **Artifact Distribution:** Done. 
*   **Workflow:** Done (Transactional updates with rollback and health verification).
*   **Auto-Restart:** Done (Worker updates its own binary securely and restarts).

## 2. Environment Bootstrapping & Parity (IMPLEMENTED)
**Status:** IMPLEMENTED.
**Goal:** Ensure workers always have the correct CLI tools required by their capabilities.
*   **Implement `BootstrapEnvironment` Profile:** Done. Profile exists and checks for `allowBootstrap`.
*   **Package Management Integration:** Done.
*   **PATH Management:** Done.

## 3. One-Click UI Job Templates (IMPLEMENTED)
**Status:** IMPLEMENTED.
**Goal:** Move away from raw YAML manifest entry in the dashboard and provide standard, clickable playbooks.
*   **Template Library:** Done (ASCII game, Go Test Suite, etc.).
*   **Dynamic Forms:** Done.

## 4. Enhanced Messaging & Telemetry (PARTIALLY IMPLEMENTED)
**Status:** PARTIALLY IMPLEMENTED / IN PROGRESS.
**Goal:** Improve visibility into exactly what the runners are doing.
*   **Real-time Output:** Done.
*   **Deep Agent Integration:** Done (AgentBridge communications layer implemented and tested on Fedora).
