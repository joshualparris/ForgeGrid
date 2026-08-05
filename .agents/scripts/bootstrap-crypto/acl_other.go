//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func replaceFileAtomically(tempPath, destinationPath string) error {
	return os.Rename(tempPath, destinationPath)
}

func secureBootstrapDirectory(dir string) error {
	return os.MkdirAll(dir, 0700)
}

func writeSecureBlob(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := secureBootstrapDirectory(dir); err != nil {
		return fmt.Errorf("failed to secure directory: %w", err)
	}

	f, err := os.CreateTemp(dir, "secure-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tempPath := f.Name()

	if err := os.Chmod(tempPath, 0600); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to secure temp file: %w", err)
	}

	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to write secrets: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to sync secrets: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := replaceFileAtomically(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to replace file: %w", err)
	}

	return nil
}
