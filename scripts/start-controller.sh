#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/forgegrid/logs"
LOGIN_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/forgegrid/coordinator/dashboard-login.txt"
mkdir -p "$LOG_DIR"

cd "$ROOT"

if ! pgrep -f "$ROOT/forgegrid agent-bridge serve --port 9091" >/dev/null 2>&1; then
  setsid "$ROOT/forgegrid" agent-bridge serve --port 9091 >>"$LOG_DIR/agentbridge.log" 2>&1 < /dev/null &
fi

if ! pgrep -f "$ROOT/forgegrid -mode coordinator -port 8080" >/dev/null 2>&1; then
  setsid "$ROOT/forgegrid" -mode coordinator -port 8080 >>"$LOG_DIR/coordinator.log" 2>&1 < /dev/null &
fi

for _ in {1..30}; do
  if [ -s "$LOGIN_FILE" ]; then
    break
  fi
  sleep 0.5
done

"$ROOT/forgegrid" session start -controller https://127.0.0.1:8080 -ip 10.245.173.178 -agent-port 9091 || true

if command -v xdg-open >/dev/null 2>&1; then
  xdg-open "https://10.245.173.178:8080" >/dev/null 2>&1 || true
fi

echo
echo "ForgeGrid controller startup requested."
echo
if [ -s "$LOGIN_FILE" ]; then
  cat "$LOGIN_FILE"
else
  echo "Dashboard login file was not ready yet."
  echo "Check the coordinator log for the password:"
  echo "  $LOG_DIR/coordinator.log"
fi
echo
echo "Logs:"
echo "  $LOG_DIR/agentbridge.log"
echo "  $LOG_DIR/coordinator.log"
echo
echo "Press Enter to close this window."
read -r _
