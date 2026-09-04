package manifest

import (
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Project    string          `yaml:"project"`
	Repository Repository      `yaml:"repository"`
	Tasks      map[string]Task `yaml:"tasks"`
}

type Repository struct {
	URL        string `yaml:"url"`
	BaseCommit string `yaml:"base_commit"`
	Branch     string `yaml:"branch"`
	CreatePR   bool   `yaml:"create_pr"`
	PRTitle    string `yaml:"pr_title"`
	PRBody     string `yaml:"pr_body"`
}

type Task struct {
	Description  string       `yaml:"description"`
	Requirements Requirements `yaml:"requirements"`
	Execution    Execution    `yaml:"execution,omitempty"`
	Stages       []Execution  `yaml:"stages,omitempty"`
	Artefacts    []string     `yaml:"artefacts"`
}

type Requirements struct {
	MinRAMGB     int      `yaml:"min_ram_gb"`
	OS           string   `yaml:"os"`
	Architecture string   `yaml:"architecture"`
	MinCores     int      `yaml:"min_cores"`
	Labels       []string `yaml:"labels"`
	Capabilities []string `yaml:"capabilities"`
}

type Execution struct {
	Name           string            `yaml:"name,omitempty"`
	Profile        string            `yaml:"profile"`
	Parameters     map[string]string `yaml:"parameters"`
	Tools          []string          `yaml:"tools,omitempty"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	Changes        Changes           `yaml:"changes,omitempty"`
	MaxRetries     int               `yaml:"max_retries,omitempty"`
}

type Changes struct {
	Commit        bool   `yaml:"commit"`
	Push          bool   `yaml:"push"`
	CommitMessage string `yaml:"commit_message"`
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
		if len(task.Stages) == 0 {
			if strings.TrimSpace(task.Execution.Profile) == "" {
				return nil, fmt.Errorf("task '%s' must define an execution profile or stages", name)
			}
			task.Execution.Name = "Execution"
			task.Stages = []Execution{task.Execution}
		}

		for i, stage := range task.Stages {
			if strings.TrimSpace(stage.Profile) == "" {
				return nil, fmt.Errorf("task '%s' stage %d must define an execution profile", name, i)
			}
			if stage.Changes.Push && !stage.Changes.Commit {
				return nil, fmt.Errorf("task '%s' stage %d cannot push changes unless commit is true", name, i)
			}
			if stage.Changes.CommitMessage != "" && !stage.Changes.Commit {
				return nil, fmt.Errorf("task '%s' stage %d defines a commit message but commit is false", name, i)
			}
			if stage.MaxRetries < 0 || stage.MaxRetries > 3 {
				return nil, fmt.Errorf("task '%s' stage %d max_retries must be between 0 and 3", name, i)
			}
			if stage.Name == "" {
				task.Stages[i].Name = fmt.Sprintf("Stage %d", i+1)
			}
		}
		m.Tasks[name] = task
	}

	return &m, nil
}
