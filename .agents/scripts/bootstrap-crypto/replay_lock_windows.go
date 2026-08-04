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

		if err != windows.ERROR_LOCK_VIOLATION && err != windows.ERROR_IO_PENDING {
			return fmt.Errorf("unexpected lock error: %w", err)
		}

		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for lock: %w", err)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !locked {
		return fmt.Errorf("failed to acquire lock")
	}
	return nil
}

var unlockFile = func(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
