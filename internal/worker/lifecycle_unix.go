//go:build !windows

package worker

import (
	"os"
	"os/exec"
)

func detectLifecycleOS() string {
	if os.Getenv("INVOCATION_ID") != "" || os.Getenv("JOURNAL_STREAM") != "" {
		return "systemd"
	}
	return "portable"
}

type systemdLifecycle struct{}

func newSystemdLifecycle() Lifecycle {
	return &systemdLifecycle{}
}

func (l *systemdLifecycle) Mode() string {
	return "systemd"
}

func (l *systemdLifecycle) Start(tx *UpdateTransaction) error {
	// systemctl --user start forgegrid-worker.service
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

func newWindowsServiceLifecycle() Lifecycle {
	// Not applicable on Unix
	return newPortableLifecycle()
}
