# ForgeGrid Release Notes

## Status Update
ForgeGrid has undergone a massive security and stability refactor. 

## Working
- Secure, token-authenticated, self-signed TLS encrypted coordinator-worker pairing and communication.
- Hardware detection (CPU model, accurate RAM, available Workspace space, architecture, physical/logical cores, etc.) via `gopsutil`.
- Random challenge tests generating worker-side SHA-256 verifications.
- Job persistence and reliable recovery.
- Sensitive credentials omitted from all HTTP/API endpoints and UI.
- Secure, one-time, expiry-driven pairing codes.
- Windows & Linux native builds with verification scripts.

## Tested
- Full automated test suite encompassing TLS connections, rate limiting, expiry, token separation, hardware stats mock, and job verification checks (`TEST_RESULTS.txt`).
- Local integration verification across Linux simulated networks (`verify-linux.sh`).
- *Pending Physical LAN Verification (See `REAL_LAN_TEST.md`)*

## Not Yet Tested
- Physical Windows worker testing (requires manual user execution over LAN).

## Not Implemented
- Godot integration.
- Git repository syncing.
- AI Agent dispatch logic.
- Complex multi-stage job manifests.

## Security Limitations
- `InsecureSkipVerify` is used on the worker, but the certificate is **pinned** strictly to the SHA-256 fingerprint displayed on the coordinator screen. This is a secure approach for local-network TLS where root CAs are impractical, but requires the user to accurately transfer the fingerprint.
- Port 8080/8443 must be opened on firewalls; the app relies strictly on the isolated LAN environment and does not perform network traversal outside of the local subnet.
