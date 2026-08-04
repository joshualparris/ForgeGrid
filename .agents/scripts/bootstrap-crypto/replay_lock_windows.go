//go:build windows

package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func lockFile(f *os.File) error {
	timeout := time.After(5 * time.Second)
	locked := false
	for {
		var ol windows.Overlapped
		err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
		if err == nil {
			locked = true
			break
		}
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for lock")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !locked {
		return fmt.Errorf("failed to acquire lock")
	}
	return nil
}

func unlockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
