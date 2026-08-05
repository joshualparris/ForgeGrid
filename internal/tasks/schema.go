package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

type Task struct {
	TaskID             string   `json:"task_id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	BaseBranch         string   `json:"base_branch"`
	TaskBranch         string   `json:"task_branch"`
	AllowedPaths       []string `json:"allowed_paths"`
	ForbiddenPaths     []string `json:"forbidden_paths"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	TestProfile        string   `json:"test_profile"`
	TestArguments      []string `json:"test_arguments"`
	Dependencies       []string `json:"dependencies"`
	RunnerLabels       []string `json:"runner_labels"`
	TimeoutMinutes     int      `json:"timeout_minutes"`
	MaxRetries         int      `json:"max_retries"`
}

type Plan struct {
	Tasks []Task `json:"tasks"`
}

func DecodePlan(data []byte) (Plan, error) {
	var plan Plan
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	if decoder.More() {
		return plan, errors.New("plan contains trailing data")
	}
	return plan, ValidatePlan(plan)
}

func ValidatePlan(plan Plan) error {
	if len(plan.Tasks) == 0 {
		return errors.New("plan has no tasks")
	}
	ids, branches := map[string]bool{}, map[string]bool{}
	for _, task := range plan.Tasks {
		if !safeID.MatchString(task.TaskID) {
			return fmt.Errorf("unsafe task id %q", task.TaskID)
		}
		if ids[task.TaskID] {
			return fmt.Errorf("duplicate task id %q", task.TaskID)
		}
		ids[task.TaskID] = true
		if task.TaskBranch != "task/"+task.TaskID {
			return fmt.Errorf("unsafe task branch %q", task.TaskBranch)
		}
		branchKey := strings.ToLower(task.TaskBranch)
		if branches[branchKey] {
			return fmt.Errorf("duplicate task branch %q", task.TaskBranch)
		}
		branches[branchKey] = true
		if task.BaseBranch != "main" {
			return fmt.Errorf("base branch must be main")
		}
		if len(task.AllowedPaths) == 0 {
			return fmt.Errorf("task %s has no allowed paths", task.TaskID)
		}
		if len(task.AcceptanceCriteria) == 0 {
			return fmt.Errorf("task %s has no acceptance criteria", task.TaskID)
		}
		if len(strings.Fields(task.Description)) < 4 {
			return fmt.Errorf("task %s description is too vague", task.TaskID)
		}
		if task.TestProfile == "" {
			return fmt.Errorf("task %s has no test profile", task.TaskID)
		}
		if task.TimeoutMinutes < 1 || task.TimeoutMinutes > 120 {
			return fmt.Errorf("task %s timeout must be 1..120", task.TaskID)
		}
		if task.MaxRetries < 0 || task.MaxRetries > 3 {
			return fmt.Errorf("task %s retries must be 0..3", task.TaskID)
		}
		for _, path := range task.AllowedPaths {
			normal := normalise(path)
			if strings.HasPrefix(normal, ".github/") || strings.HasPrefix(normal, ".forgegrid/") || normal == "agents.md" {
				return fmt.Errorf("task %s may not own protected path %s", task.TaskID, path)
			}
		}
	}
	for _, task := range plan.Tasks {
		for _, dependency := range task.Dependencies {
			if !ids[dependency] {
				return fmt.Errorf("task %s has unknown dependency %s", task.TaskID, dependency)
			}
		}
	}
	if err := detectCycle(plan.Tasks); err != nil {
		return err
	}
	for i := 0; i < len(plan.Tasks); i++ {
		for j := i + 1; j < len(plan.Tasks); j++ {
			if independent(plan.Tasks[i], plan.Tasks[j]) && overlaps(plan.Tasks[i].AllowedPaths, plan.Tasks[j].AllowedPaths) {
				return fmt.Errorf("parallel tasks %s and %s overlap", plan.Tasks[i].TaskID, plan.Tasks[j].TaskID)
			}
		}
	}
	return nil
}

func normalise(value string) string {
	return strings.ToLower(filepath.ToSlash(strings.TrimSpace(value)))
}
func fixedPrefix(value string) string {
	value = normalise(value)
	if i := strings.IndexAny(value, "*?["); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSuffix(value, "/")
}
func overlaps(left, right []string) bool {
	for _, a := range left {
		for _, b := range right {
			x, y := fixedPrefix(a), fixedPrefix(b)
			if x == "" || y == "" || x == y || strings.HasPrefix(x, y+"/") || strings.HasPrefix(y, x+"/") {
				return true
			}
		}
	}
	return false
}
func independent(a, b Task) bool {
	for _, id := range a.Dependencies {
		if id == b.TaskID {
			return false
		}
	}
	for _, id := range b.Dependencies {
		if id == a.TaskID {
			return false
		}
	}
	return true
}
func detectCycle(all []Task) error {
	dependencies := map[string][]string{}
	for _, task := range all {
		dependencies[task.TaskID] = task.Dependencies
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("dependency cycle at %s", id)
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, dependency := range dependencies[id] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	ids := make([]string, 0, len(dependencies))
	for id := range dependencies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
