package execution

import (
	"runtime"
	"testing"
)

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
