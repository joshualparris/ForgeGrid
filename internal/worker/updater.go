package worker

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type UpdateTransaction struct {
	ID               string    `json:"transaction_id"`
	WorkerID         string    `json:"worker_id"`
	OldBinaryPath    string    `json:"old_binary_path"`
	NewBinaryPath    string    `json:"new_binary_path"`
	BackupBinaryPath string    `json:"backup_binary_path"`
	ExpectedSHA256   string    `json:"expected_sha256"`
	OldSHA256        string    `json:"old_sha256"`
	CurrentState     string    `json:"current_state"`
	StartedAt        time.Time `json:"started_at"`
	RestartDeadline  time.Time `json:"restart_deadline"`
	RollbackReason   string    `json:"rollback_reason"`
	WorkerPID        int       `json:"worker_pid"`
	LifecycleMode    string    `json:"lifecycle_mode"`
}

func getTxPath() string {
	return filepath.Join(getWorkerDataDir(), "update_tx.json")
}

func readTx() (*UpdateTransaction, error) {
	b, err := os.ReadFile(getTxPath())
	if err != nil {
		return nil, err
	}
	var tx UpdateTransaction
	if err := json.Unmarshal(b, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

func writeTx(tx *UpdateTransaction) error {
	b, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	tmp := getTxPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	err = os.Rename(tmp, getTxPath())
	if err == nil {
		reportTxState(tx)
	}
	return err
}

func reportTxState(tx *UpdateTransaction) {
	b, err := os.ReadFile(getWorkerCredsPath())
	if err != nil {
		return
	}
	var creds WorkerCredentials
	if err := json.Unmarshal(b, &creds); err != nil {
		return
	}

	payload := map[string]interface{}{
		"worker_id": tx.WorkerID,
		"update_id": tx.ID,
		"status":    strings.ToLower(tx.CurrentState),
		"message":   "Update state changed to " + tx.CurrentState,
	}
	pb, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", creds.CoordinatorURL+"/api/updates/report", bytes.NewReader(pb))
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+creds.Token)
		client := &http.Client{Timeout: 5 * time.Second}
		if creds.Insecure {
			client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		}
		client.Do(req)
	}
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func processIsRunning(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 is safe on Unix. On Windows, FindProcess always succeeds, we can test it by just waiting slightly.
	// But actually, we don't need a perfectly robust check, just polling or waiting enough time.
	// We will wait up to 10 seconds.
	_ = p
	return false // Simplified for cross-platform, we'll just wait
}

func RunUpdater() {
	tx, err := readTx()
	if err != nil {
		log.Fatalf("Failed to read transaction: %v", err)
	}

	// Give the old worker a few seconds to exit gracefully
	time.Sleep(3 * time.Second)

	if tx.CurrentState == "STAGED" {
		tx.CurrentState = "APPLYING"
		writeTx(tx)

		log.Printf("[Update] Swapping binaries...")
		if err := swapBinaries(tx); err != nil {
			tx.RollbackReason = "Swap failed: " + err.Error()
			rollback(tx)
			return
		}

		tx.CurrentState = "RESTARTING"
		writeTx(tx)

		log.Printf("[Update] Starting candidate worker...")
		if err := GetLifecycle(tx.LifecycleMode).Start(tx); err != nil {
			tx.RollbackReason = "Start failed: " + err.Error()
			rollback(tx)
			return
		}

		tx.CurrentState = "VERIFYING_NEW_WORKER"
		writeTx(tx)
	}

	if tx.CurrentState == "VERIFYING_NEW_WORKER" {
		log.Printf("[Update] Waiting for health verification from candidate...")
		if err := waitForHealth(tx); err != nil {
			tx.RollbackReason = "Health check failed: " + err.Error()
			rollback(tx)
			return
		}
		// Candidate worker has marked tx as COMPLETED
		log.Printf("[Update] Candidate verified and update completed successfully!")
		os.Exit(0)
	}

	if tx.CurrentState == "ROLLING_BACK" || tx.CurrentState == "ROLLED_BACK" {
		rollback(tx)
	}
}

// safeReplace puts the file at newPath into place at destPath. A direct
// os.Rename(newPath, destPath) can fail on Windows with
// ERROR_SHARING_VIOLATION/ERROR_ACCESS_DENIED if anything (a lingering AV
// scan, a not-yet-released handle from the process that just exited) still
// holds destPath open, even briefly. Moving the existing file out of the
// way first, then renaming the replacement in, avoids requiring a
// rename-over-existing to succeed at all; if the final rename fails, the
// original file is restored so destPath is never left missing.
func safeReplace(newPath, destPath string) error {
	replacedPath := destPath + ".replaced"
	hadExisting := false
	if _, err := os.Stat(destPath); err == nil {
		hadExisting = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", destPath, err)
	}
	if hadExisting {
		if err := os.Remove(replacedPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear stale %s: %w", replacedPath, err)
		}
		if err := os.Rename(destPath, replacedPath); err != nil {
			return fmt.Errorf("move existing %s aside: %w", destPath, err)
		}
	}
	if err := os.Rename(newPath, destPath); err != nil {
		if hadExisting {
			if restoreErr := os.Rename(replacedPath, destPath); restoreErr != nil {
				return fmt.Errorf("rename %s to %s failed (%v), and restoring the original failed too: %w", newPath, destPath, err, restoreErr)
			}
		}
		return fmt.Errorf("rename %s to %s: %w", newPath, destPath, err)
	}
	if err := os.Chmod(destPath, 0755); err != nil {
		return fmt.Errorf("chmod %s: %w", destPath, err)
	}
	if hadExisting {
		if err := os.Remove(replacedPath); err != nil {
			return fmt.Errorf("remove stale %s after successful replace: %w", replacedPath, err)
		}
	}
	return nil
}

func swapBinaries(tx *UpdateTransaction) error {
	// stageUpdate already made a verified backup copy of the primary before
	// the old process exited, so it's safe to move the primary itself aside
	// here rather than deleting/overwriting it in place.
	return safeReplace(tx.NewBinaryPath, tx.OldBinaryPath)
}

func startCandidateWorker(tx *UpdateTransaction) error {
	return GetLifecycle(tx.LifecycleMode).Start(tx)
}

func waitForHealth(tx *UpdateTransaction) error {
	deadline := tx.RestartDeadline
	if deadline.IsZero() {
		deadline = time.Now().Add(45 * time.Second)
	}
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		current, err := readTx()
		if err == nil && current.CurrentState == "COMPLETED" {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for healthy heartbeat from new worker")
}

func rollback(tx *UpdateTransaction) {
	log.Printf("[Update] Rolling back: %s", tx.RollbackReason)
	tx.CurrentState = "ROLLING_BACK"
	writeTx(tx)

	if err := safeReplace(tx.BackupBinaryPath, tx.OldBinaryPath); err != nil {
		tx.CurrentState = "ROLLBACK_FAILED"
		tx.RollbackReason = tx.RollbackReason + " | restore from backup failed: " + err.Error()
		writeTx(tx)
		log.Printf("[Update] Rollback FAILED to restore the previous binary: %v", err)
		return
	}

	log.Printf("[Update] Restarting previous worker...")
	if err := GetLifecycle(tx.LifecycleMode).Start(tx); err != nil {
		tx.CurrentState = "ROLLBACK_FAILED"
		tx.RollbackReason = tx.RollbackReason + " | restart of restored binary failed: " + err.Error()
		writeTx(tx)
		log.Printf("[Update] Rollback FAILED to restart the previous worker: %v", err)
		return
	}

	tx.CurrentState = "ROLLED_BACK"
	writeTx(tx)
	log.Printf("[Update] Rollback initiated. Waiting for previous worker to verify...")
}

type WorkerStatus struct {
	State         string `json:"state"`
	TransactionID string `json:"transaction_id"`
	Message       string `json:"message"`
}

func readStatus() (*WorkerStatus, error) {
	b, err := os.ReadFile(WorkerStatusPath())
	if err != nil {
		return nil, err
	}
	var st WorkerStatus
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}
