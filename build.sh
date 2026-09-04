#!/bin/bash
set -euo pipefail

VERSION="${FORGEGRID_VERSION:-0.8.0}"
COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo dev)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X forgegrid/internal/version.Version=${VERSION} -X forgegrid/internal/version.Commit=${COMMIT} -X forgegrid/internal/version.BuildTime=${BUILD_TIME}"

echo "Building ForgeGrid..."
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o forgegrid main.go
GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS" -o dist/ForgeGrid-USB/Windows/ForgeGrid.exe main.go
GOOS=windows GOARCH=386 go build -ldflags "$LDFLAGS" -o dist/ForgeGrid-USB/Windows/ForgeGrid-x86.exe main.go
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o dist/ForgeGrid-USB/Linux/forgegrid main.go
(
  cd dist/ForgeGrid-USB
  sha256sum Windows/ForgeGrid.exe Windows/ForgeGrid-x86.exe Linux/forgegrid Windows/START-FORGEGRID.bat Linux/start-forgegrid.sh README.html Examples/forgegrid.yaml > CHECKSUMS.txt
  WIN_AMD64_SHA="$(sha256sum Windows/ForgeGrid.exe | awk '{print $1}')"
  WIN_X86_SHA="$(sha256sum Windows/ForgeGrid-x86.exe | awk '{print $1}')"
  LINUX_SHA="$(sha256sum Linux/forgegrid | awk '{print $1}')"
  WIN_AMD64_SIZE="$(stat -c%s Windows/ForgeGrid.exe)"
  WIN_X86_SIZE="$(stat -c%s Windows/ForgeGrid-x86.exe)"
  LINUX_SIZE="$(stat -c%s Linux/forgegrid)"
  cat > update-manifest.json <<EOF
{
  "schema_version": "1",
  "product": "ForgeGrid",
  "version": "${VERSION}",
  "commit": "${COMMIT}",
  "generated_at": "${BUILD_TIME}",
  "minimum_coordinator_version": "0.8.0",
  "minimum_worker_version": "0.8.0",
  "protocol": "1",
  "artifacts": [
    {
      "role": "worker",
      "platform": "windows",
      "architecture": "amd64",
      "sha256": "${WIN_AMD64_SHA}",
      "path": "Windows/ForgeGrid.exe",
      "size": ${WIN_AMD64_SIZE}
    },
    {
      "role": "worker",
      "platform": "windows",
      "architecture": "386",
      "sha256": "${WIN_X86_SHA}",
      "path": "Windows/ForgeGrid-x86.exe",
      "size": ${WIN_X86_SIZE}
    },
    {
      "role": "worker",
      "platform": "linux",
      "architecture": "amd64",
      "sha256": "${LINUX_SHA}",
      "path": "Linux/forgegrid",
      "size": ${LINUX_SIZE}
    }
  ]
}
EOF
)
echo "Build complete."
