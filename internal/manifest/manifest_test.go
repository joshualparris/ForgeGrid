package manifest

import (
	"strings"
	"testing"
)

func TestParseManifest_Success(t *testing.T) {
	yamlData := `
project: "TestProject"
tasks:
  build:
    description: "Build task"
    requirements:
      min_ram_gb: 4
      os: "linux"
    commands:
      linux: "./build.sh"
      windows: "build.bat"
    artefacts:
      - "dist/*"
`
	m, err := Parse(strings.NewReader(yamlData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Project != "TestProject" {
		t.Errorf("expected TestProject, got %s", m.Project)
	}
	if len(m.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(m.Tasks))
	}
	
	task := m.Tasks["build"]
	if task.Requirements.MinRAMGB != 4 {
		t.Errorf("expected 4, got %d", task.Requirements.MinRAMGB)
	}
	if task.Commands.Linux != "./build.sh" {
		t.Errorf("expected ./build.sh, got %s", task.Commands.Linux)
	}
	if len(task.Artefacts) != 1 || task.Artefacts[0] != "dist/*" {
		t.Errorf("expected artefacts ['dist/*'], got %v", task.Artefacts)
	}
}

func TestParseManifest_Invalid(t *testing.T) {
	yamlData := `
project: "TestProject"
tasks:
  build:
    commands:
      linux: ""
`
	// Missing both linux and windows commands entirely is an error? Or at least one is required.
	_, err := Parse(strings.NewReader(yamlData))
	if err == nil {
		t.Error("expected error for missing commands, got nil")
	}
}
