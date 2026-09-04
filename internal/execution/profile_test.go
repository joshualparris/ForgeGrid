package execution

import (
	"runtime"
	"testing"
)

func TestVersionProfilesAreReadOnlyAndTakeNoArguments(t *testing.T) {
	for name, wantExecutable := range map[string]string{
		"GitVersion":    "git",
		"PythonVersion": "python",
		"GoVersion":     "go",
		"NodeVersion":   "node",
	} {
		p, ok := Profiles[name]
		if !ok {
			t.Fatalf("expected profile %s to be registered", name)
		}
		if p.Executable != wantExecutable {
			t.Fatalf("%s: expected executable %q, got %q", name, wantExecutable, p.Executable)
		}
		if len(p.ArgKeys) != 0 {
			t.Fatalf("%s: version-check profiles must not accept caller-supplied arguments, got ArgKeys=%v", name, p.ArgKeys)
		}
		if p.AcceptsTools {
			t.Fatalf("%s: version-check profiles must not accept the tools list", name)
		}
		// Mock pinning so this test doesn't depend on what's installed on
		// whatever machine happens to run go test.
		pinnedExecutables[name] = wantExecutable
		profile, err := GetProfile(name)
		if err != nil {
			t.Fatalf("GetProfile(%s) failed: %v", name, err)
		}
		args, err := BuildArgs(profile, nil, nil)
		if err != nil {
			t.Fatalf("BuildArgs(%s) failed: %v", name, err)
		}
		if len(args) != len(p.Subcommand) {
			t.Fatalf("%s: expected exactly the fixed subcommand %v, got %v", name, p.Subcommand, args)
		}
	}
}

func TestBootstrapEnvironmentProfile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("BootstrapEnvironment is Windows-only")
	}

	// Mock the executable pinning since winget might not exist in the test env
	pinnedExecutables["BootstrapEnvironment"] = "winget"
	profile, err := GetProfile("BootstrapEnvironment")
	if err != nil {
		t.Fatalf("Failed to get BootstrapEnvironment profile: %v", err)
	}

	if !profile.AcceptsTools {
		t.Fatalf("Expected BootstrapEnvironment to accept tools")
	}

	params := map[string]string{}
	tools := []string{"godot", "python"}

	args, err := BuildArgs(profile, params, tools)
	if err != nil {
		t.Fatalf("BuildArgs failed: %v", err)
	}

	expectedArgCount := len(profile.Subcommand) + 2
	if len(args) != expectedArgCount {
		t.Fatalf("Expected %d arguments, got %d", expectedArgCount, len(args))
	}

	if args[len(args)-2] != "GodotEngine.GodotEngine" || args[len(args)-1] != "Python.Python.3.11" {
		t.Fatalf("Expected tools godot and python to be mapped to package names, got %v", args)
	}

	// Test flag injection prevention
	tools = []string{"-unsafe"}
	_, err = BuildArgs(profile, params, tools)
	if err == nil {
		t.Fatalf("Expected BuildArgs to fail when tool starts with a hyphen")
	}
}
