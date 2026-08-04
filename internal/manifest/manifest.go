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
	Commands     Commands     `yaml:"commands"`
	Artefacts    []string     `yaml:"artefacts"`
}

type Requirements struct {
	MinRAMGB int    `yaml:"min_ram_gb"`
	OS       string `yaml:"os"`
	MinCores int    `yaml:"min_cores"`
}

type Commands struct {
	Windows string `yaml:"windows"`
	Linux   string `yaml:"linux"`
}

func Parse(r io.Reader) (*Manifest, error) {
	var m Manifest
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true) // strict parsing
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
		if strings.TrimSpace(task.Commands.Linux) == "" && strings.TrimSpace(task.Commands.Windows) == "" {
			return nil, fmt.Errorf("task '%s' must define at least one command (linux or windows)", name)
		}
	}

	return &m, nil
}
