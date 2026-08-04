//go:build windows

package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func applySecureACL(path string, isDir bool) error {
	tok, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("failed to open process token: %w", err)
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return fmt.Errorf("failed to get token user: %w", err)
	}
	sid := u.User.Sid.String()

	inherit := ""
	if isDir {
		inherit = "OICI;"
	}

	sddl := "D:PAI(A;" + inherit + "FA;;;" + sid + ")(A;" + inherit + "FA;;;SY)(A;" + inherit + "FA;;;BA)"
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("failed to create security descriptor: %w", err)
	}

	dacl, _, _ := sd.DACL()

	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("SetNamedSecurityInfo failed: %w", err)
	}

	return verifySecureACL(path, sid)
}

func verifySecureACL(path, expectedSID string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("GetNamedSecurityInfo failed: %w", err)
	}
	if sd == nil {
		return fmt.Errorf("no security descriptor returned")
	}

	sddl := sd.String()

	if !strings.Contains(sddl, "D:P") {
		return fmt.Errorf("DACL is not protected (inheritance not removed): %s", sddl)
	}

	allowedSIDs := map[string]bool{
		expectedSID: true,
		"SY":        true,
		"BA":        true,
	}

	daclPart := sddl
	if idx := strings.Index(daclPart, "S:"); idx != -1 {
		daclPart = daclPart[:idx]
	}
	if idx := strings.Index(daclPart, "G:"); idx != -1 {
		daclPart = daclPart[:idx]
	}
	if idx := strings.Index(daclPart, "O:"); idx != -1 {
		daclPart = daclPart[:idx]
	}

	parts := strings.Split(daclPart, "(")
	for _, p := range parts[1:] {
		ace := strings.TrimRight(p, ")")
		fields := strings.Split(ace, ";")
		if len(fields) >= 6 {
			aceType := fields[0]
			accountSid := fields[5]
			if aceType == "A" { // Allow ACE
				if !allowedSIDs[accountSid] {
					return fmt.Errorf("unauthorized SID found in DACL: %s", accountSid)
				}
			}
		}
	}
	return nil
}

func replaceFileAtomically(tempPath, destinationPath string) error {
	tempPtr, err := windows.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	destPtr, err := windows.UTF16PtrFromString(destinationPath)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(tempPtr, destPtr, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return err
	}
	return nil
}

func writeSecureConfig(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	if err := applySecureACL(dir, true); err != nil {
		return fmt.Errorf("failed to secure directory: %w", err)
	}

	tempPath := path + ".tmp"
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}

	if err := applySecureACL(tempPath, false); err != nil {
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
		return fmt.Errorf("failed to replace temp file: %w", err)
	}

	tok, _ := windows.OpenCurrentProcessToken()
	defer tok.Close()
	u, _ := tok.GetTokenUser()
	sid := u.User.Sid.String()
	if err := verifySecureACL(path, sid); err != nil {
		os.Remove(path)
		return fmt.Errorf("failed to verify final file ACLs: %w", err)
	}

	return nil
}
