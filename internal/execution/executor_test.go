package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecutor_TimeoutAndCancellation(t *testing.T) {
	executor := NewExecutor()

	tmpDir := t.TempDir()
	sleeperSrc := filepath.Join(tmpDir, "sleeper.go")
	sleeperBin := filepath.Join(tmpDir, "sleeper")
	if runtime.GOOS == "windows" {
		sleeperBin += ".exe"
	}

	src := `package main
import "time"
func main() { time.Sleep(10 * time.Second) }`
	if err := os.WriteFile(sleeperSrc, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write sleeper src: %v", err)
	}

	cmd := exec.Command("go", "build", "-o", sleeperBin, sleeperSrc)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build sleeper: %v", err)
	}

	profile := Profile{
		Name:           "sleeper",
		Executable:     sleeperBin,
		MaxTimeoutSecs: 10,
		Subcommand:     []string{},
		ArgKeys:        []string{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	out, err := executor.Execute(ctx, profile, map[string]string{}, nil, tmpDir)

	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected cancellation error, got %v", err)
	}
	_ = out
}

func TestSecureWorkspacePath_Negative(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{"Valid path", "foo", false},
		{"Valid nested", "foo/bar", false},
		{"Escape attempt up", "../escaped", true},
		{"Escape attempt root", "/etc/passwd", true},
		{"Windows escape", "C:\\Windows\\System32", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "Windows escape" && runtime.GOOS != "windows" {
				t.Skip("Skipping windows escape test on non-windows platform")
			}
			if tt.name == "Escape attempt root" && runtime.GOOS == "windows" {
				t.Skip("Skipping Unix absolute path test on windows platform")
			}
			_, err := SecureWorkspacePath(tmpDir, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("SecureWorkspacePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
