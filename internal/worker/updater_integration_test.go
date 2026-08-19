package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdaterIntegrationSuccessfulUpdate(t *testing.T) {
	// 35. Local deterministic successful-update test
	tmp := t.TempDir()
	os.Setenv("USERPROFILE", tmp)
	os.Setenv("HOME", tmp)

	updateDir := filepath.Join(getWorkerDataDir(), "updates")
	os.MkdirAll(updateDir, 0755)

	// Create fake candidate that just exits 0 to simulate success (in a real scenario it would report health)
	candidatePath := filepath.Join(tmp, "candidate.exe")
	os.WriteFile(candidatePath, []byte("#!/bin/sh\nexit 0\n"), 0755)

	primaryPath := filepath.Join(tmp, "ForgeGrid.exe")
	backupPath := filepath.Join(tmp, "previous-ForgeGrid.exe")
	os.WriteFile(primaryPath, []byte("#!/bin/sh\necho old\n"), 0755)
	os.WriteFile(backupPath, []byte("#!/bin/sh\necho old\n"), 0755)

	tx := &UpdateTransaction{
		ID:               "tx-123",
		CurrentState:     "STAGED",
		OldBinaryPath:    primaryPath,
		NewBinaryPath:    candidatePath,
		BackupBinaryPath: backupPath,
		LifecycleMode:    "portable",
		RestartDeadline:  time.Now().Add(5 * time.Second),
	}
	writeTx(tx)

	// We don't call RunUpdater directly as it blocks or exits, we test its components manually or run it in a goroutine
	// Here we simulate RunUpdater's flow for successful update:
	tx.CurrentState = "APPLYING"
	writeTx(tx)
	
	err := swapBinaries(tx)
	if err != nil {
		t.Fatalf("Swap failed: %v", err)
	}

	tx.CurrentState = "RESTARTING"
	writeTx(tx)
	err = GetLifecycle(tx.LifecycleMode).Start(tx)
	if err != nil {
		t.Fatalf("Candidate start failed: %v", err)
	}

	tx.CurrentState = "VERIFYING_NEW_WORKER"
	writeTx(tx)

	// Simulate candidate reporting health
	tx.CurrentState = "COMPLETED"
	writeTx(tx)

	err = waitForHealth(tx)
	if err != nil {
		t.Fatalf("Wait for health failed: %v", err)
	}
}

func TestUpdaterIntegrationRollback(t *testing.T) {
	// 36. Local deterministic rollback test
	tmp := t.TempDir()
	os.Setenv("USERPROFILE", tmp)
	os.Setenv("HOME", tmp)

	updateDir := filepath.Join(getWorkerDataDir(), "updates")
	os.MkdirAll(updateDir, 0755)

	candidatePath := filepath.Join(tmp, "candidate.exe")
	os.WriteFile(candidatePath, []byte("#!/bin/sh\nexit 1\n"), 0755)

	primaryPath := filepath.Join(tmp, "ForgeGrid.exe")
	backupPath := filepath.Join(tmp, "previous-ForgeGrid.exe")
	os.WriteFile(primaryPath, []byte("#!/bin/sh\necho old\n"), 0755)
	os.WriteFile(backupPath, []byte("#!/bin/sh\necho backup\n"), 0755)

	tx := &UpdateTransaction{
		ID:               "tx-rollback",
		CurrentState:     "VERIFYING_NEW_WORKER", // Skip to verifying
		OldBinaryPath:    primaryPath,
		NewBinaryPath:    candidatePath,
		BackupBinaryPath: backupPath,
		LifecycleMode:    "portable",
		RestartDeadline:  time.Now().Add(1 * time.Second),
	}
	writeTx(tx)

	// Wait for health should fail due to timeout
	err := waitForHealth(tx)
	if err == nil {
		t.Fatalf("Expected wait for health to timeout/fail")
	}

	// It should then rollback
	rollback(tx)

	// Primary should be backup
	b, _ := os.ReadFile(primaryPath)
	if string(b) != "#!/bin/sh\necho backup\n" {
		t.Fatalf("Primary was not restored: %s", string(b))
	}
}
