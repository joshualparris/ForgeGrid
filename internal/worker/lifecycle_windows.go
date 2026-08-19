//go:build windows

package worker

import (
	"os"
	"os/exec"
	"golang.org/x/sys/windows/svc"
)

func detectLifecycleOS() string {
	isInteractive, err := svc.IsAnInteractiveSession()
	if err == nil && !isInteractive {
		return "windows-service"
	}
	return "portable"
}

type windowsServiceLifecycle struct{}

func newWindowsServiceLifecycle() Lifecycle {
	return &windowsServiceLifecycle{}
}

func (l *windowsServiceLifecycle) Mode() string {
	return "windows-service"
}

func (l *windowsServiceLifecycle) Start(tx *UpdateTransaction) error {
	// The transaction expects us to start the service using the SCM.
	// ControlService ("start") uses the SCM to start the service.
	// But we need to pass the txID via environment variable. 
	// SCM doesn't let us easily pass environment variables to a specific start.
	// However, the worker reads `update_tx.json` on startup automatically if it is in VERIFYING_NEW_WORKER or RESTARTING state!
	// So we don't strictly NEED the FORGEGRID_UPDATE_TX env var if we check for an active transaction on every boot.
	// Let's modify verifyUpdateTransaction to just read the file if it exists and is in RESTARTING/VERIFYING_NEW_WORKER state.
	return ControlService("start", nil)
}

type portableLifecycle struct{}

func newPortableLifecycle() Lifecycle {
	return &portableLifecycle{}
}

func (l *portableLifecycle) Mode() string {
	return "portable"
}

func (l *portableLifecycle) Start(tx *UpdateTransaction) error {
	cmd := exec.Command(tx.OldBinaryPath, "-mode", "worker")
	cmd.Env = append(os.Environ(), "FORGEGRID_UPDATE_TX="+tx.ID)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func newSystemdLifecycle() Lifecycle {
	// Not applicable on Windows, fallback to portable
	return newPortableLifecycle()
}
