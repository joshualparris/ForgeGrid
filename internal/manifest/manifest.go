package manifest

import (
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Project string          `yaml:"project"`
	Tasks   map[string]Task `yaml:"tasks"`
}

type Task struct {
	Description  string       `yaml:"description"`
	Requirements Requirements `yaml:"requirements"`
	Execution    Execution    `yaml:"execution"`
	Artefacts    []string     `yaml:"artefacts"`
}

type Requirements struct {
	MinRAMGB int    `yaml:"min_ram_gb"`
	OS       string `yaml:"os"`
	MinCores int    `yaml:"min_cores"`
}

type Execution struct {
	Profile        string            `yaml:"profile"`
	Args           []string          `yaml:"args"`
	Env            map[string]string `yaml:"env"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
}

func Parse(r io.Reader) (*Manifest, error) {
	var m Manifest
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true)
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	if m.Project == "" {
		return nil, fmt.Errorf("project name is required")
	}

	if len(m.Tasks) == 0 {
		return nil, fmt.Errorf("at least one task is required")
	}

	for name, task := range m.Tasks {
		if strings.TrimSpace(task.Execution.Profile) == "" {
			return nil, fmt.Errorf("task '%s' must define an execution profile", name)
		}
	}

	return &m, nil
}
