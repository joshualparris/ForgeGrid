#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/forgegrid/logs"
mkdir -p "$LOG_DIR"

cd "$ROOT"

if ! pgrep -f "forgegrid agent-bridge serve --port 9091" >/dev/null 2>&1; then
  nohup "$ROOT/forgegrid" agent-bridge serve --port 9091 >>"$LOG_DIR/agentbridge.log" 2>&1 &
fi

if ! pgrep -f "forgegrid -mode coordinator -port 8080" >/dev/null 2>&1; then
  nohup "$ROOT/forgegrid" -mode coordinator -port 8080 >>"$LOG_DIR/coordinator.log" 2>&1 &
fi

sleep 1
"$ROOT/forgegrid" session start -controller https://127.0.0.1:8080 -ip 10.245.173.178 -agent-port 9091 || true

if command -v xdg-open >/dev/null 2>&1; then
  xdg-open "https://10.245.173.178:8080" >/dev/null 2>&1 || true
fi

echo
echo "ForgeGrid controller startup requested."
echo "Logs:"
echo "  $LOG_DIR/agentbridge.log"
echo "  $LOG_DIR/coordinator.log"

