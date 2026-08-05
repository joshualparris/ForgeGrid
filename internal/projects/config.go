package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var (
	projectIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

type Config struct {
	SchemaVersion      int               `json:"schema_version"`
	ProjectID          string            `json:"project_id"`
	Repository         string            `json:"repository"`
	DefaultBranch      string            `json:"default_branch"`
	VisibilityRequired string            `json:"visibility_required"`
	ExecutionProfile   string            `json:"execution_profile"`
	RunnerLabels       []string          `json:"runner_labels"`
	TestProfile        string            `json:"test_profile"`
	TestArguments      []string          `json:"test_arguments"`
	Environment        map[string]string `json:"environment"`
	LockedPaths        []string          `json:"locked_paths"`
	TaskBranchPrefix   string            `json:"task_branch_prefix"`
	DraftPullRequests  bool              `json:"draft_pull_requests"`
	AutomaticMerge     bool              `json:"automatic_merge"`
}

func Decode(reader io.Reader) (Config, error) {
	var config Config
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("decode project config: %w", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return config, errors.New("project config contains trailing data")
	}
	return config, Validate(config)
}

func Validate(config Config) error {
	if config.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema version %d", config.SchemaVersion)
	}
	if !projectIDPattern.MatchString(config.ProjectID) {
		return fmt.Errorf("unsafe project id %q", config.ProjectID)
	}
	if !repositoryPattern.MatchString(config.Repository) {
		return fmt.Errorf("invalid repository %q", config.Repository)
	}
	if config.DefaultBranch != "main" {
		return errors.New("default branch must be main")
	}
	if config.VisibilityRequired != "private" {
		return errors.New("AI coding project must require private visibility")
	}
	if config.ExecutionProfile == "" || config.TestProfile == "" {
		return errors.New("execution and test profiles are required")
	}
	if len(config.RunnerLabels) == 0 {
		return errors.New("runner labels are required")
	}
	if config.TaskBranchPrefix != "task/" {
		return errors.New("task branch prefix must be task/")
	}
	if !config.DraftPullRequests {
		return errors.New("draft pull requests must be required")
	}
	if config.AutomaticMerge {
		return errors.New("automatic merge must be disabled")
	}
	for key := range config.Environment {
		if key == "PATH" || strings.EqualFold(key, "GITHUB_TOKEN") || strings.EqualFold(key, "GH_TOKEN") {
			return fmt.Errorf("environment override %s is forbidden", key)
		}
	}
	return nil
}
