package execution

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestExecutorTimeout(t *testing.T) {
	ws := t.TempDir()

	// Create a dummy profile
	prof := Profile{
		Name: "test-timeout",
	}

	if runtime.GOOS == "windows" {
		prof.Executable = "powershell.exe"
	} else {
		prof.Executable = "sh"
	}

	executor := NewExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var args []string
	if runtime.GOOS == "windows" {
		// Run a powershell sleep that spawns a child process and waits
		args = []string{"-Command", "Start-Sleep -Seconds 5"}
	} else {
		args = []string{"-c", "sleep 5"}
	}

	start := time.Now()
	res := executor.Run(ctx, prof, args, nil, ws)
	duration := time.Since(start)

	if duration > 2*time.Second {
		t.Fatalf("Executor failed to timeout properly, took %v", duration)
	}

	if res.Error == nil {
		t.Errorf("Expected error from timeout, got nil")
	}
}

func TestExecutorCancellationAndChildTermination(t *testing.T) {
	ws := t.TempDir()

	// Write a script that spawns a child process which writes to a file constantly
	scriptPath := filepath.Join(ws, "testscript")
	outPath := filepath.Join(ws, "out.txt")

	if runtime.GOOS == "windows" {
		scriptPath += ".bat"
		os.WriteFile(scriptPath, []byte(`
@echo off
powershell.exe -Command "while($true) { Add-Content -Path '`+outPath+`' -Value 'alive'; Start-Sleep -Milliseconds 100 }"
`), 0755)
	} else {
		scriptPath += ".sh"
		os.WriteFile(scriptPath, []byte(`#!/bin/sh
sh -c 'while true; do echo alive >> "`+outPath+`"; sleep 0.1; done'
`), 0755)
	}

	prof := Profile{
		Name: "test-script",
	}
	if runtime.GOOS == "windows" {
		prof.Executable = "cmd.exe"
	} else {
		prof.Executable = "sh"
	}

	executor := NewExecutor()
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		// Wait up to 5 seconds for file to be created
		for i := 0; i < 50; i++ {
			if _, err := os.Stat(outPath); err == nil {
				time.Sleep(200 * time.Millisecond) // Give it time to write a bit more
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		cancel()
	}()

	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"/c", scriptPath}
	} else {
		args = []string{scriptPath}
	}

	res := executor.Run(ctx, prof, args, nil, ws)

	if res.Error == nil {
		t.Errorf("Expected error from cancellation, got nil")
	}

	// Read outPath size, wait a bit, read again to ensure the child process is dead
	info1, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("Child process didn't start or write to file: %v", err)
	}
	size1 := info1.Size()

	time.Sleep(500 * time.Millisecond)

	info2, _ := os.Stat(outPath)
	size2 := info2.Size()

	if size1 != size2 {
		t.Errorf("Child process seems to still be alive! size1: %d, size2: %d", size1, size2)
	}
}
