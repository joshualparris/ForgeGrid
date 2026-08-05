//go:build windows
// +build windows

package execution

import (
	"context"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func setupCmd(cmd *exec.Cmd) {
	// Handled post-start
}

func manageProcess(ctx context.Context, cmd *exec.Cmd) func() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return func() {}
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)

	if cmd.Process != nil {
		handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
		if err == nil {
			windows.AssignProcessToJobObject(job, handle)
			windows.CloseHandle(handle)
		}
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Closing the handle terminates all processes in the job object
			// because of JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
			windows.CloseHandle(job)
		case <-done:
			// Job completed normally, close handle to release resources
			windows.CloseHandle(job)
		}
	}()

	return func() {
		close(done)
	}
}
