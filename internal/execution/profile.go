package execution

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Profile struct {
	Name           string
	Executable     string
	MaxTimeoutSecs int
	Subcommand     []string
	ArgKeys        []string // which keys from parameters are allowed as positional arguments
}

var pinnedExecutables = make(map[string]string)

var Profiles = map[string]Profile{
	"GoTest": {
		Name:           "GoTest",
		Executable:     "go",
		MaxTimeoutSecs: 600,
		Subcommand:     []string{"test", "-v"},
		ArgKeys:        []string{"package"}, // Only the "package" parameter is appended
	},
	"NodeBuild": {
		Name:           "NodeBuild",
		Executable:     "npm",
		MaxTimeoutSecs: 600,
		Subcommand:     []string{"run", "build"},
		ArgKeys:        []string{}, // No additional args
	},
	"NodeTest": {
		Name:           "NodeTest",
		Executable:     "npm",
		MaxTimeoutSecs: 600,
		Subcommand:     []string{"test"},
		ArgKeys:        []string{},
	},
	"NodeLint": {
		Name:           "NodeLint",
		Executable:     "npm",
		MaxTimeoutSecs: 600,
		Subcommand:     []string{"run", "lint"},
		ArgKeys:        []string{},
	},
	"GoBuild": {
		Name:           "GoBuild",
		Executable:     "go",
		MaxTimeoutSecs: 600,
		Subcommand:     []string{"build"},
		ArgKeys:        []string{"package"},
	},
	"PythonUnittest": {
		Name:           "PythonUnittest",
		Executable:     "python",
		MaxTimeoutSecs: 300,
		Subcommand:     []string{"-m", "unittest"},
		ArgKeys:        []string{"module"},
	},

	"AIAgent": {
		Name:           "AIAgent",
		Executable:     "antigravity",
		MaxTimeoutSecs: 3600, // 1 hour max for agent tasks
		Subcommand:     []string{"--task"},
		ArgKeys:        []string{"task"},
	},
	"GodotExport": {
		Name:           "GodotExport",
		Executable:     "godot",
		MaxTimeoutSecs: 1800,
		Subcommand:     []string{"--headless", "--export-release"},
		ArgKeys:        []string{"preset", "export_path"},
	},
	"CodexExec": {
		Name:           "CodexExec",
		Executable:     "codex",
		MaxTimeoutSecs: 3600,
		Subcommand:     []string{"exec", "--sandbox", "workspace-write"},
		ArgKeys:        []string{"prompt"},
	},
}

func init() {
	// Pin executables on initialization
	for _, p := range Profiles {
		path, err := exec.LookPath(p.Executable)
		if err == nil {
			pinnedExecutables[p.Name] = path
		}
	}
}

func GetProfile(name string) (Profile, error) {
	p, ok := Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("execution profile not found: %s", name)
	}
	pinnedPath, ok := pinnedExecutables[p.Name]
	if !ok {
		return Profile{}, fmt.Errorf("executable for profile %s was not found during initialization", name)
	}
	p.Executable = pinnedPath
	return p, nil
}

func BuildArgs(p Profile, params map[string]string) ([]string, error) {
	args := append([]string{}, p.Subcommand...)
	for _, key := range p.ArgKeys {
		if val, ok := params[key]; ok && val != "" {
			if strings.HasPrefix(val, "-") {
				return nil, fmt.Errorf("parameter %s cannot start with a hyphen to prevent flag injection", key)
			}
			args = append(args, val)
		}
	}
	return args, nil
}

func SecureWorkspacePath(workspaceRoot, targetPath string) (string, error) {
	if filepath.IsAbs(targetPath) {
		return "", fmt.Errorf("fail closed: absolute target paths are not allowed: %s", targetPath)
	}

	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("fail closed: absolute workspace root error: %w", err)
	}

	evalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		evalRoot = absRoot
	}

	joined := filepath.Join(evalRoot, targetPath)
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("fail closed: absolute joined path error: %w", err)
	}

	evalJoined, err := filepath.EvalSymlinks(absJoined)
	if err == nil {
		absJoined = evalJoined
	}

	// Case sensitive containment check (Linux requirement, and strict Windows)
	rootPrefix := filepath.Clean(evalRoot) + string(filepath.Separator)
	joinedClean := filepath.Clean(absJoined)

	if joinedClean != filepath.Clean(evalRoot) && !strings.HasPrefix(joinedClean, rootPrefix) {
		return "", fmt.Errorf("fail closed: path escapes workspace (case-sensitive check): %s", targetPath)
	}

	// Windows specific junction/reparse point validation
	if runtime.GOOS == "windows" {
		fi, err := os.Lstat(absJoined)
		if err == nil {
			if fi.Mode()&os.ModeSymlink != 0 || fi.Mode()&os.ModeIrregular != 0 {
				return "", fmt.Errorf("fail closed: Windows reparse point/junction detected inside target path")
			}
		}
	}

	return absJoined, nil
}
