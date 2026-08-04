# Physical LAN Test Results

**Test Date and Time:** 2026-08-04 11:05 AEST (Local Time)
**Fedora Hostname:** josh-hp-mini
**Fedora IP:** 10.245.173.178
**Windows IP:** N/A (Not visible from worker endpoint)

### Git Verification
- **Fedora Git SHA:** 55521c22c63b689231d5ebb68995c38d06e8207c
- **Windows Git SHA:** 55521c22c63b689231d5ebb68995c38d06e8207c
- **SHAs Matched:** Yes

### Subsystem Results
- **Coordinator Startup Result:** Success (Started gracefully, generated TLS keys)
- **TLS Result:** Success (Pinned fingerprint matched and was enforced)
- **Connectivity Result:** Success (Heartbeats received accurately)
- **Worker Hardware Result:** Success (Windows 10, Intel i3-7130U, 4 Logical Processors, 8.4GB RAM)
- **First Job Result:** Success (SHA-256 challenge correctly generated and returned)
- **Offline Transition Result:** Success (Detected offline status after worker stoppage)
- **Credential Reconnection Result:** Success (Worker re-authenticating seamlessly via saved config without a code)
- **Same-Worker-ID Result:** Success (Reused ID `worker-57f8fa66ee79ad1e58e1694b9ffec817`)
- **Second Job Result:** Success (Second SHA-256 test completed flawlessly post-reconnect)
- **Windows Free-Disk Result:** Success (~33.6 GB workspace detected securely)

### Known Defects
- `VERIFY-WINDOWS.bat` produced an input-redirection error but incorrectly reported success. This test script is fundamentally flawed and must return non-zero exit codes upon command failures.

### Tests Not Performed
- Multiple concurrent worker scaling tests.
- Physical execution of complex workflows (e.g. Godot engine builds).
