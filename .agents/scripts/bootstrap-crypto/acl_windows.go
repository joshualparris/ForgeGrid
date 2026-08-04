//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

func secureBootstrapDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create bootstrap dir: %w", err)
	}
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	cmdDir := exec.Command("icacls", dir, "/inheritance:r", "/grant", "*"+u.Uid+":F", "*S-1-5-18:F", "*S-1-5-32-544:F")
	if err := cmdDir.Run(); err != nil {
		return fmt.Errorf("failed to set bootstrap directory ACLs: %w", err)
	}

	verifyCmd := exec.Command("icacls", dir)
	out, err := verifyCmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), u.Username) {
		return fmt.Errorf("failed to verify bootstrap directory ACLs")
	}

	return nil
}
