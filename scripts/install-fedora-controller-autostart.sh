#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
USER_SYSTEMD="$HOME/.config/systemd/user"
APPLICATIONS="$HOME/.local/share/applications"
DESKTOP="$HOME/Desktop"
LOG_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/forgegrid/logs"

mkdir -p "$USER_SYSTEMD" "$APPLICATIONS" "$LOG_DIR"

cat >"$USER_SYSTEMD/forgegrid-agentbridge.service" <<UNIT
[Unit]
Description=ForgeGrid AgentBridge Relay
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$ROOT
ExecStart=$ROOT/forgegrid agent-bridge serve --port 9091
Restart=always
RestartSec=5
StandardOutput=append:$LOG_DIR/agentbridge.log
StandardError=append:$LOG_DIR/agentbridge.log

[Install]
WantedBy=default.target
UNIT

cat >"$USER_SYSTEMD/forgegrid-coordinator.service" <<UNIT
[Unit]
Description=ForgeGrid Coordinator
After=network-online.target forgegrid-agentbridge.service
Wants=network-online.target forgegrid-agentbridge.service

[Service]
Type=simple
WorkingDirectory=$ROOT
ExecStart=$ROOT/forgegrid -mode coordinator -port 8080
Restart=always
RestartSec=5
StandardOutput=append:$LOG_DIR/coordinator.log
StandardError=append:$LOG_DIR/coordinator.log

[Install]
WantedBy=default.target
UNIT

cat >"$APPLICATIONS/forgegrid-controller.desktop" <<DESKTOP_FILE
[Desktop Entry]
Type=Application
Name=Start ForgeGrid Controller
Comment=Start ForgeGrid coordinator and AgentBridge relay
Exec=$ROOT/scripts/start-controller.sh
Terminal=true
Categories=Development;
DESKTOP_FILE

chmod +x "$ROOT/scripts/start-controller.sh" "$APPLICATIONS/forgegrid-controller.desktop"

if [ -d "$DESKTOP" ]; then
  cp "$APPLICATIONS/forgegrid-controller.desktop" "$DESKTOP/forgegrid-controller.desktop"
  chmod +x "$DESKTOP/forgegrid-controller.desktop"
fi

systemctl --user daemon-reload
systemctl --user enable forgegrid-agentbridge.service forgegrid-coordinator.service

echo "Installed Fedora controller auto-start."
echo
echo "Start now:"
echo "  systemctl --user start forgegrid-agentbridge.service forgegrid-coordinator.service"
echo
echo "Desktop launcher:"
echo "  $APPLICATIONS/forgegrid-controller.desktop"
if [ -d "$DESKTOP" ]; then
  echo "  $DESKTOP/forgegrid-controller.desktop"
fi

