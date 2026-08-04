//go:build !windows

package main

import "os"

// For non-Windows builds, simply provide empty or placeholder lock file methods
// since this agent client bootstrap utility is only needed on Windows.

func lockFile(f *os.File) error {
	return nil
}

func unlockFile(f *os.File) error {
	return nil
}
