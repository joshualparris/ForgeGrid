//go:build windows

package agentbridge

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

func writeSecureConfig(path string, b []byte) error {
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user for ACL: %w", err)
	}

	cmdDir := exec.Command("icacls", dir, "/inheritance:r", "/grant", "*"+u.Uid+":F", "*S-1-5-18:F", "*S-1-5-32-544:F")
	if err := cmdDir.Run(); err != nil {
		return fmt.Errorf("failed to set directory ACLs: %w", err)
	}

	tempPath := path + ".tmp"
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}

	cmdFile := exec.Command("icacls", tempPath, "/inheritance:r", "/grant", "*"+u.Uid+":F", "*S-1-5-18:F", "*S-1-5-32-544:F")
	if err := cmdFile.Run(); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to set temp file ACLs: %w", err)
	}

	verifyCmd := exec.Command("icacls", tempPath)
	out, err := verifyCmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), u.Username) {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to verify temp file ACLs")
	}

	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to write secrets: %w", err)
	}

	f.Sync()
	f.Close()

	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	verifyFinal := exec.Command("icacls", path)
	outFinal, err := verifyFinal.CombinedOutput()
	if err != nil || !strings.Contains(string(outFinal), u.Username) {
		os.Remove(path)
		return fmt.Errorf("failed to verify final file ACLs")
	}

	return nil
}
