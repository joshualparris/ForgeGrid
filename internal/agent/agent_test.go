package agent

import (
	"context"
	"strings"
	"testing"
)

func TestProviderRegistry(t *testing.T) {
	p, err := GetProvider("codex")
	if err != nil || p.ID() != "codex" {
		t.Fatalf("Expected codex provider, got: %v", err)
	}

	p, err = GetProvider("antigravity")
	if err != nil || p.ID() != "antigravity" {
		t.Fatalf("Expected antigravity provider, got: %v", err)
	}

	p, err = GetProvider("fake")
	if err != nil || p.ID() != "fake" {
		t.Fatalf("Expected fake provider, got: %v", err)
	}

	_, err = GetProvider("unknown")
	if err == nil {
		t.Fatalf("Expected error for unknown provider")
	}

	providers := RegisteredProviders()
	if len(providers) < 3 {
		t.Fatalf("Expected at least 3 providers, got %d", len(providers))
	}
}

func TestAntigravityInvocation(t *testing.T) {
	p, _ := GetProvider("antigravity")
	req := AgentRequest{
		Task:               "Fix the bug",
		ProjectName:        "TestProject",
		SafetyInstructions: "Be safe",
	}
	inv, err := p.BuildInvocation(req)
	if err != nil {
		t.Fatalf("Failed to build invocation: %v", err)
	}

	if len(inv.Args) < 2 {
		t.Fatalf("Expected arguments, got: %v", inv.Args)
	}
	if inv.Args[0] != "--dangerously-skip-permissions" {
		t.Fatalf("Expected --dangerously-skip-permissions, got: %s", inv.Args[0])
	}
	if inv.Args[1] != "--print" {
		t.Fatalf("Expected --print, got: %s", inv.Args[1])
	}

	promptArg := inv.Args[2]
	if !strings.Contains(promptArg, "TestProject") || !strings.Contains(promptArg, "Fix the bug") || !strings.Contains(promptArg, "Be safe") {
		t.Fatalf("Prompt argument missing expected content: %s", promptArg)
	}
}

func TestCodexInvocation(t *testing.T) {
	p, _ := GetProvider("codex")
	req := AgentRequest{
		Task:               "Fix the bug",
		ProjectName:        "TestProject",
		SafetyInstructions: "Be safe",
	}
	inv, err := p.BuildInvocation(req)
	if err != nil {
		t.Fatalf("Failed to build invocation: %v", err)
	}

	if inv.Executable != "codex" {
		t.Fatalf("Expected executable codex, got: %s", inv.Executable)
	}
	if inv.Args[0] != "exec" {
		t.Fatalf("Expected exec, got: %s", inv.Args[0])
	}
}

func TestAgentDetection(t *testing.T) {
	ctx := context.Background()

	pFake, _ := GetProvider("fake")
	det := pFake.Detect(ctx)
	if !det.Available {
		t.Fatalf("Expected fake provider to be available")
	}

	// For real providers, we just ensure Detect doesn't panic and returns a valid struct
	pAg, _ := GetProvider("antigravity")
	det = pAg.Detect(ctx)
	if det.Reason == "" {
		t.Fatalf("Expected reason to be set")
	}

	pCodex, _ := GetProvider("codex")
	det = pCodex.Detect(ctx)
	if det.Reason == "" {
		t.Fatalf("Expected reason to be set")
	}
}
