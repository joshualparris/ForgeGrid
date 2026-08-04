# Real LAN Test Results (2026-08-04)

- Fedora IP: 10.245.173.178
- Windows IP: 10.245.173.221
- both machines used Git SHA: 55521c22c63b689231d5ebb68995c38d06e8207c
- Windows worker ID: worker-57f8fa66ee79ad1e58e1694b9ffec817
- Windows hardware:
  - Windows 10 Pro Education
  - Intel i3-7130U
  - approximately 8 GB RAM
  - approximately 33.6 GB free workspace disk
- pairing and heartbeat connection worked
- worker reconnected using its saved identity
- two SHA-256 test jobs were started
- the Windows console showed both jobs starting
- coordinator-side completion output and exact hashes were not independently preserved in the evidence
- VERIFY-WINDOWS.bat produced:
  `ERROR: Input redirection is not supported`
  but still incorrectly reported success
- multi-worker scaling and complex build workflows were not tested
