//go:build !windows

package agentbridge

import (
	"os"
	"path/filepath"
)

func replaceFileAtomically(tempPath, destinationPath string) error {
	return os.Rename(tempPath, destinationPath)
}

func writeSecureConfig(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tempPath := path + ".tmp"
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tempPath)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tempPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return err
	}
	if err := replaceFileAtomically(tempPath, path); err != nil {
		os.Remove(tempPath)
		return err
	}
	return nil
}
