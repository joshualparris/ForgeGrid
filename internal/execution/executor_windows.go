package execution

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobObjectLimitKillOnJobClose = uint32(0x2000)
	modntdll                     = windows.NewLazySystemDLL("ntdll.dll")
	procNtResumeProcess          = modntdll.NewProc("NtResumeProcess")
)

func configureOSProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_SUSPENDED,
	}
}

func startProcess(cmd *exec.Cmd) (func(), error) {
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start process suspended: %w", err)
	}

	jobHandle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		terminateProcess(cmd)
		return nil, fmt.Errorf("failed to create job object: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: jobObjectLimitKillOnJobClose,
		},
	}

	_, err = windows.SetInformationJobObject(
		jobHandle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		terminateProcess(cmd)
		windows.CloseHandle(jobHandle)
		return nil, fmt.Errorf("failed to set job object limits: %w", err)
	}

	processHandle, err := windows.OpenProcess(windows.PROCESS_ALL_ACCESS, false, uint32(cmd.Process.Pid))
	if err != nil {
		terminateProcess(cmd)
		windows.CloseHandle(jobHandle)
		return nil, fmt.Errorf("failed to open process handle: %w", err)
	}
	defer windows.CloseHandle(processHandle)

	err = windows.AssignProcessToJobObject(jobHandle, processHandle)
	if err != nil {
		terminateProcess(cmd)
		windows.CloseHandle(jobHandle)
		return nil, fmt.Errorf("failed to assign process to job object: %w", err)
	}

	r1, _, err := procNtResumeProcess.Call(uintptr(processHandle))
	if r1 != 0 {
		terminateProcess(cmd)
		windows.CloseHandle(jobHandle)
		return nil, fmt.Errorf("NtResumeProcess failed with status %x", r1)
	}

	cleanup := func() {
		windows.CloseHandle(jobHandle)
	}
	return cleanup, nil
}

func terminateProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
