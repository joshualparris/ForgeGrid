//go:build linux || darwin
// +build linux darwin

package execution

import (
	"context"
	"os/exec"
	"syscall"
)

func setupCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func manageProcess(ctx context.Context, cmd *exec.Cmd) func() {
	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-done:
		}
	}()

	return func() {
		close(done)
	}
}
