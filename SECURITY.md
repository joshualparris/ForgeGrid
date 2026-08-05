# ForgeGrid Security & Threat Model

## Philosophy
ForgeGrid executes arbitrary processes (builds, tests) on connected machines. Security is paramount to prevent lateral movement, arbitrary RCE outside the workspace, and unauthorized cluster control.

## Access Control & Authentication
- **LAN-Only Operation**: The application binds strictly to local network interfaces by default.
- **Pairing Process**: Workers pair with the Coordinator using an expiring 6-digit numeric code.
- **Identity & Encryption**: The Coordinator generates a temporary, self-signed CA and TLS certificates for the cluster. The certificate fingerprint is shown during pairing for verification.
- **Tokens**: Once paired, each worker receives a unique authentication token. Tokens are verified using constant-time comparison.

## Workspace Security
- **Path Restrictions**: Commands are restricted strictly to the designated `ForgeGrid` workspace folder.
- **Traversal Protection**: All paths provided by the Coordinator (for files and artefacts) are sanitized to prevent directory traversal (e.g., `../../`).
- **Command Limitations**:
  - Arbitrary remote shell access is forbidden.
  - No remote shutdown or OS administration commands are allowed in v1.
  - "Destructive-looking" commands (like `rm -rf`) require explicit allow-lists and confirmation.

## Network Protection
- **Encrypted Transport**: All communication (HTTP/WebSockets) operates over TLS, mitigating man-in-the-middle attacks on the local network.
- **Size Limits**: Maximum upload sizes and artefact size limits are enforced to prevent memory exhaustion and DoS attacks.
- **Secrets Redaction**: Environment variables or logs containing sensitive tokens are redacted before streaming to the Coordinator.

## Privacy
- **No Telemetry**: No usage data is collected or sent anywhere.
- **No Cloud Services**: Fully offline capable, requiring zero internet access.
