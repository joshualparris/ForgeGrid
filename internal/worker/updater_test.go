package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdaterTransaction(t *testing.T) {
	tmp := t.TempDir()
	os.Setenv("USERPROFILE", tmp)
	os.Setenv("HOME", tmp)

	tx := &UpdateTransaction{
		ID:               "tx-123",
		CurrentState:     "STAGED",
		ExpectedSHA256:   "abcd",
		LifecycleMode:    "portable",
		RestartDeadline:  time.Now().Add(60 * time.Second),
	}
	
	// Create mock updates dir
	os.MkdirAll(filepath.Join(getWorkerDataDir(), "updates"), 0755)

	err := writeTx(tx)
	if err != nil {
		t.Fatalf("Failed to write tx: %v", err)
	}

	read, err := readTx()
	if err != nil || read.ID != "tx-123" {
		t.Fatalf("Failed to read tx: %v", err)
	}
}

func TestUpdaterCleanup(t *testing.T) {
	tmp := t.TempDir()
	os.Setenv("USERPROFILE", tmp)
	os.Setenv("HOME", tmp)

	w := &Worker{}
	updateDir := filepath.Join(getWorkerDataDir(), "updates")
	os.MkdirAll(updateDir, 0755)

	// Create dummy helper
	helperPath := filepath.Join(updateDir, "updater-helper-forgegrid")
	os.WriteFile(helperPath, []byte("fake"), 0755)

	// No active tx
	w.cleanupUpdateFiles()

	if _, err := os.Stat(helperPath); !os.IsNotExist(err) {
		t.Fatalf("Helper was not cleaned up")
	}
}

func TestRecoveryStates(t *testing.T) {
	tmp := t.TempDir()
	os.Setenv("USERPROFILE", tmp)
	os.Setenv("HOME", tmp)

	updateDir := filepath.Join(getWorkerDataDir(), "updates")
	os.MkdirAll(updateDir, 0755)
	
	primary := filepath.Join(tmp, "primary.exe")
	backup := filepath.Join(tmp, "previous-primary.exe")
	candidate := filepath.Join(tmp, "candidate.exe")

	os.WriteFile(primary, []byte("primary"), 0755)
	os.WriteFile(backup, []byte("backup"), 0755)
	os.WriteFile(candidate, []byte("candidate"), 0755)

	tx := &UpdateTransaction{
		ID:               "tx-123",
		CurrentState:     "STAGED",
		OldBinaryPath:    primary,
		NewBinaryPath:    candidate,
		BackupBinaryPath: backup,
		LifecycleMode:    "portable",
		RestartDeadline:  time.Now().Add(60 * time.Second),
	}
	
	err := swapBinaries(tx)
	if err != nil {
		t.Fatalf("Failed to swap: %v", err)
	}
	
	b, _ := os.ReadFile(primary)
	if string(b) != "candidate" {
		t.Fatalf("Primary is not candidate")
	}

	tx.RollbackReason = "test"
	rollback(tx) // tests rollback idempotence

	b, _ = os.ReadFile(primary)
	if string(b) != "backup" {
		t.Fatalf("Primary is not backup")
	}
}
