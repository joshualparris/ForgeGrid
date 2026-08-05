package execution

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Profile struct {
	Name       string
	Executable string
}

var Profiles = map[string]Profile{
	"go":     {Name: "go", Executable: "go"},
	"node":   {Name: "node", Executable: "node"},
	"python": {Name: "python", Executable: "python"},
	"test":   {Name: "test", Executable: "echo"}, // for testing
}

func GetProfile(name string) (Profile, error) {
	p, ok := Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("execution profile not found: %s", name)
	}
	path, err := exec.LookPath(p.Executable)
	if err != nil {
		return Profile{}, fmt.Errorf("executable for profile %s not found in PATH: %w", name, err)
	}
	p.Executable = path
	return p, nil
}

func SecureWorkspacePath(workspaceRoot, targetPath string) (string, error) {
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", err
	}

	evalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		evalRoot = absRoot
	}

	joined := filepath.Join(evalRoot, targetPath)
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}

	evalJoined, err := filepath.EvalSymlinks(absJoined)
	if err == nil {
		absJoined = evalJoined
	}

	// Prefix check with case-insensitivity (for Windows compatibility, safe on Linux too for just prefix logic if we assume workspace is canonical)
	rootPrefix := filepath.Clean(evalRoot) + string(filepath.Separator)
	joinedClean := filepath.Clean(absJoined)

	if joinedClean != filepath.Clean(evalRoot) && !strings.HasPrefix(strings.ToLower(joinedClean), strings.ToLower(rootPrefix)) {
		return "", fmt.Errorf("path escapes workspace: %s", targetPath)
	}

	return absJoined, nil
}
