package execution

import (
	"os/exec"
	"syscall"
)

func configureOSProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func startProcess(cmd *exec.Cmd) (func(), error) {
	err := cmd.Start()
	return nil, err
}

func terminateProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
