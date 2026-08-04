//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// For non-Windows builds, we use unix.Flock for advisory locking.
func lockFile(f *os.File) error {
	timeout := time.After(5 * time.Second)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return fmt.Errorf("unexpected lock error: %v", err)
		}
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for lock")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
