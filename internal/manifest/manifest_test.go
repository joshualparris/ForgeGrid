package manifest

import (
	"strings"
	"testing"
)

func TestParseManifest(t *testing.T) {
	yamlData := `
project: "ForgeGrid"
tasks:
  build:
    description: "Build the project"
    requirements:
      os: "linux"
    execution:
      profile: "go"
      args: ["build", "./..."]
      env:
        GOOS: "linux"
      timeout_seconds: 300
    artefacts:
      - "bin/forgegrid"
`
	m, err := Parse(strings.NewReader(yamlData))
	if err != nil {
		t.Fatalf("Failed to parse valid manifest: %v", err)
	}

	if m.Project != "ForgeGrid" {
		t.Errorf("Expected project 'ForgeGrid', got '%s'", m.Project)
	}

	task, ok := m.Tasks["build"]
	if !ok {
		t.Fatalf("Expected 'build' task to be parsed")
	}

	if task.Execution.Profile != "go" {
		t.Errorf("Expected profile 'go', got '%s'", task.Execution.Profile)
	}
	if len(task.Execution.Args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(task.Execution.Args))
	}
}

func TestParseInvalidManifest(t *testing.T) {
	yamlData := `
project: "ForgeGrid"
tasks:
  build:
    description: "Build the project"
    execution:
      args: ["build", "./..."]
`
	_, err := Parse(strings.NewReader(yamlData))
	if err == nil {
		t.Fatalf("Expected error for missing execution profile, got nil")
	}
	if !strings.Contains(err.Error(), "must define an execution profile") {
		t.Errorf("Unexpected error message: %v", err)
	}
}
