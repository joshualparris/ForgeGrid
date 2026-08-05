//go:build windows

package main

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestApplySecureACL_Directory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "acltest-dir-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if err := applySecureACL(tempDir, true); err != nil {
		t.Fatalf("applySecureACL failed: %v", err)
	}

	sid, err := currentUserSID()
	if err != nil {
		t.Fatalf("currentUserSID failed: %v", err)
	}

	if err := verifySecureACL(tempDir, sid); err != nil {
		t.Fatalf("verifySecureACL failed: %v", err)
	}
}

func TestApplySecureACL_File(t *testing.T) {
	f, err := os.CreateTemp("", "acltest-file-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tempFile := f.Name()
	f.Close()
	defer os.Remove(tempFile)

	if err := applySecureACL(tempFile, false); err != nil {
		t.Fatalf("applySecureACL failed: %v", err)
	}

	sid, err := currentUserSID()
	if err != nil {
		t.Fatalf("currentUserSID failed: %v", err)
	}

	if err := verifySecureACL(tempFile, sid); err != nil {
		t.Fatalf("verifySecureACL failed: %v", err)
	}
}

func TestExtractExplicitDACL(t *testing.T) {
	sid, err := currentUserSID()
	if err != nil {
		t.Fatalf("currentUserSID failed: %v", err)
	}

	sddls := []string{
		"D:PAI(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", // Directory SDDL
		"D:PAI(A;;FA;;;" + sid + ")(A;;FA;;;SY)(A;;FA;;;BA)",             // File SDDL
	}

	for _, sddl := range sddls {
		sd, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			t.Fatalf("Failed to create security descriptor: %v", err)
		}

		control, _, err := sd.Control()
		if err != nil {
			t.Fatalf("Failed to read security descriptor control flags: %v", err)
		}

		if control&windows.SE_DACL_PRESENT == 0 {
			t.Errorf("SE_DACL_PRESENT is not set for SDDL: %s", sddl)
		}

		dacl, defaulted, err := sd.DACL()
		if err != nil {
			t.Fatalf("Failed to extract DACL: %v", err)
		}

		if dacl == nil {
			t.Errorf("DACL is nil for SDDL: %s", sddl)
		}

		if defaulted {
			t.Errorf("defaulted is true, expected false for SDDL: %s", sddl)
		}

		extracted, err := extractExplicitDACL(sd)
		if err != nil {
			t.Fatalf("extractExplicitDACL failed: %v for SDDL: %s", err, sddl)
		}

		if extracted == nil {
			t.Errorf("extractExplicitDACL returned nil ACL for SDDL: %s", sddl)
		}
	}
}
