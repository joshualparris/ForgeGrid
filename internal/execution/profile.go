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
	AcceptsTools   bool     // if true, passes the tools slice as positional args
}

var ApprovedBootstrapTools = map[string]string{
	"go":     "GoLang.Go",
	"node":   "OpenJS.NodeJS",
	"python": "Python.Python.3.11",
	"godot":  "GodotEngine.GodotEngine",
	"gh":     "GitHub.cli",
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
	"GoRaceTest": {
		Name:           "GoRaceTest",
		Executable:     "go",
		MaxTimeoutSecs: 900,
		Subcommand:     []string{"test", "-race", "-v"},
		ArgKeys:        []string{"package"},
	},
	"PythonCompile": {
		Name:           "PythonCompile",
		Executable:     "python",
		MaxTimeoutSecs: 300,
		Subcommand:     []string{"-m", "compileall", "-q"},
		ArgKeys:        []string{"path"},
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
		ArgKeys:        []string{"prompt", "task"},
	},
	"GodotExport": {
		Name:           "GodotExport",
		Executable:     "godot",
		MaxTimeoutSecs: 1800,
		Subcommand:     []string{"--headless", "--export-release"},
		ArgKeys:        []string{"preset", "export_path"},
	},
	"BootstrapEnvironment": {
		Name:           "BootstrapEnvironment",
		Executable:     "winget",
		MaxTimeoutSecs: 3600,
		Subcommand:     []string{"install", "--accept-package-agreements", "--accept-source-agreements", "-e"},
		ArgKeys:        []string{},
		AcceptsTools:   true,
	},
}

func init() {
	// Pin executables on initialization
	for _, p := range Profiles {
		for _, candidate := range executableCandidates(p.Executable) {
			path, err := exec.LookPath(candidate)
			if err == nil {
				pinnedExecutables[p.Name] = path
				break
			}
			if filepath.IsAbs(candidate) {
				if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
					pinnedExecutables[p.Name] = candidate
					break
				}
			}
		}
	}
}

func executableCandidates(name string) []string {
	if name != "antigravity" {
		return []string{name}
	}
	var candidates []string
	if env := strings.TrimSpace(os.Getenv("ANTIGRAVITY_PATH")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates, "antigravity", "antigravity.exe")
	for _, root := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(root, "Programs", "Antigravity", "Antigravity.exe"),
			filepath.Join(root, "Antigravity", "Antigravity.exe"),
			filepath.Join(root, "Google", "Antigravity", "Antigravity.exe"),
		)
	}
	return candidates
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

func BuildArgs(p Profile, params map[string]string, tools []string) ([]string, error) {
	if p.Name == "BootstrapEnvironment" && runtime.GOOS != "windows" {
		return nil, fmt.Errorf("BootstrapEnvironment is currently unsupported on non-Windows environments")
	}

	args := append([]string{}, p.Subcommand...)
	for _, key := range p.ArgKeys {
		if val, ok := params[key]; ok && val != "" {
			if strings.HasPrefix(val, "-") {
				return nil, fmt.Errorf("parameter %s cannot start with a hyphen to prevent flag injection", key)
			}
			args = append(args, val)
		}
	}
	if p.AcceptsTools {
		for _, tool := range tools {
			if strings.HasPrefix(tool, "-") {
				return nil, fmt.Errorf("tool %s cannot start with a hyphen", tool)
			}
			if p.Name == "BootstrapEnvironment" {
				if pkg, ok := ApprovedBootstrapTools[tool]; ok {
					args = append(args, pkg)
				} else {
					return nil, fmt.Errorf("tool %s is not an approved bootstrap package", tool)
				}
			} else {
				args = append(args, tool)
			}
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
