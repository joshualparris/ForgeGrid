package executionprofiles

import (
	"testing"
	"time"
)

func TestImmutableProfile(t *testing.T) {
	registry := DefaultRegistry()
	profile, args, err := registry.Resolve("python-unittest", []string{"discover", "-s", "tests", "-v"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Executable != "python" || len(args) < 4 {
		t.Fatalf("unexpected resolution: %#v %#v", profile, args)
	}
}
func TestRejectsShellAndTimeoutOverride(t *testing.T) {
	registry := DefaultRegistry()
	for _, arguments := range [][]string{{";", "cmd", "/c", "whoami"}, {"../../outside"}} {
		if _, _, err := registry.Resolve("python-unittest", arguments, time.Minute); err == nil {
			t.Fatalf("expected rejection: %v", arguments)
		}
	}
	if _, _, err := registry.Resolve("python-unittest", nil, 21*time.Minute); err == nil {
		t.Fatal("expected timeout ceiling rejection")
	}
}
func TestCodexTakesNoCoordinatorArguments(t *testing.T) {
	if _, _, err := DefaultRegistry().Resolve("codex-safe", []string{"--dangerously-bypass-approvals-and-sandbox"}, time.Minute); err == nil {
		t.Fatal("expected rejection")
	}
}
