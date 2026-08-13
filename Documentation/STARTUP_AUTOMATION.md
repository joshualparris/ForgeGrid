# ForgeGrid Startup Automation

Use this after the controller and runners have already been paired once.

## Fedora Controller

Install user services and a desktop launcher:

```bash
./scripts/install-fedora-controller-autostart.sh
```

Start immediately:

```bash
systemctl --user start forgegrid-agentbridge.service forgegrid-coordinator.service
```

After that, Fedora starts the coordinator and AgentBridge automatically when you log in.

You can also double-click **Start ForgeGrid Controller** from the desktop/app launcher.

## Windows Runners

From the ForgeGrid repo folder on each runner:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install-windows-runner-shortcut.ps1
```

That creates a desktop shortcut named **Start ForgeGrid Runner**.

For true boot-time auto-start, run from an Administrator PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install-windows-runner-shortcut.ps1 -InstallService
```

## Next-Time Flow

1. Turn on Fedora.
2. Turn on ThinkPad and ProBook.
3. If services are installed, wait for them to reconnect automatically.
4. Otherwise, double-click:
   - Fedora: **Start ForgeGrid Controller**
   - ThinkPad/ProBook: **Start ForgeGrid Runner**

Open the dashboard:

```text
https://10.245.173.178:8080
```

