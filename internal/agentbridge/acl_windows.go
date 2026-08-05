//go:build windows

package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func currentUserSID() (string, error) {
	tok, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return "", fmt.Errorf("failed to open current process token: %w", err)
	}
	defer tok.Close()

	u, err := tok.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("failed to get token user: %w", err)
	}
	if u == nil || u.User.Sid == nil {
		return "", fmt.Errorf("invalid token user or SID")
	}

	sid := u.User.Sid.String()
	if sid == "" {
		return "", fmt.Errorf("empty SID string")
	}

	return sid, nil
}

func extractExplicitDACL(sd *windows.SECURITY_DESCRIPTOR) (*windows.ACL, error) {
	if sd == nil {
		return nil, fmt.Errorf("nil security descriptor")
	}

	control, _, err := sd.Control()
	if err != nil {
		return nil, fmt.Errorf("failed to read security descriptor control flags: %w", err)
	}

	if control&windows.SE_DACL_PRESENT == 0 {
		return nil, fmt.Errorf("security descriptor does not contain a DACL")
	}

	dacl, defaulted, err := sd.DACL()
	if err != nil {
		return nil, fmt.Errorf("failed to extract DACL: %w", err)
	}

	if dacl == nil {
		return nil, fmt.Errorf("security descriptor contains a null DACL")
	}

	if defaulted {
		return nil, fmt.Errorf("security descriptor unexpectedly contains a defaulted DACL")
	}

	return dacl, nil
}

func applySecureACL(path string, isDir bool) error {
	sid, err := currentUserSID()
	if err != nil {
		return err
	}

	aceFlags := ""
	if isDir {
		aceFlags = "OICI"
	}

	makeACE := func(account string) string {
		return "(A;" + aceFlags + ";FA;;;" + account + ")"
	}

	sddl := "D:PAI" +
		makeACE(sid) +
		makeACE("SY") +
		makeACE("BA")
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("failed to create security descriptor: %w", err)
	}

	dacl, err := extractExplicitDACL(sd)
	if err != nil {
		return err
	}

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
	expected, err := windows.StringToSid(expectedSID)
	if err != nil {
		return fmt.Errorf("invalid expected SID %q: %w", expectedSID, err)
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
				matchesExpected := false
				if accountSD, parseErr := windows.SecurityDescriptorFromString("O:" + accountSid); parseErr == nil {
					if account, _, ownerErr := accountSD.Owner(); ownerErr == nil && account != nil {
						matchesExpected = account.Equals(expected)
					}
				}
				if !allowedSIDs[accountSid] && !matchesExpected {
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

	sid, err := currentUserSID()
	if err != nil {
		os.Remove(path)
		return fmt.Errorf("failed to obtain SID for final ACL verification: %w", err)
	}
	if err := verifySecureACL(path, sid); err != nil {
		os.Remove(path)
		return fmt.Errorf("failed to verify final file ACLs: %w", err)
	}

	return nil
}
