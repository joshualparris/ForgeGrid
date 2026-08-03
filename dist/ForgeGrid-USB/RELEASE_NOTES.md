# ForgeGrid v1.0.0 Release Notes

Welcome to the first version of ForgeGrid, the portable local-network development cluster for game development!

## Features
- **Cross-Platform**: Run on Windows and Linux x86-64 seamlessly.
- **Embedded Dashboard**: Manage your cluster from a modern web UI, fully embedded in the standalone binary. No NodeJS required!
- **Zero Configuration**: Discovery works automatically via UDP broadcast on your LAN.
- **Job Scheduling**: Distribute tasks like Godot builds and testing with resource-aware scheduling.
- **Security First**: All communications are encrypted over TLS.

## Known Limitations (MVP)
- **Node Discovery**: The current MVP hardcodes `127.0.0.1` for coordinator discovery. Full UDP broadcast discovery is documented in the architecture but pending full implementation in the network layer.
- **Job Execution Sandbox**: The actual sandboxing of the shell process is limited to running processes as the current user. True OS-level isolation (e.g. cgroups, namespaces) is not available across all target Windows/Linux environments natively without Docker.
- **Sync Protocol**: The differential file sync is heavily simplified in this MVP. For large projects, please consider using Git Mode.
- **Hardware Reporting**: Simulated static reporting is currently used in the MVP. WMI/procfs full hardware inspection is planned for v1.1.
