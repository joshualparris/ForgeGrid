package tasks

import "testing"

func validTask(id string) Task {
	return Task{TaskID: id, Title: "Title", Description: "Implement one deterministic useful feature", BaseBranch: "main", TaskBranch: "task/" + id, AllowedPaths: []string{"src/" + id + "/**"}, AcceptanceCriteria: []string{"tests pass"}, TestProfile: "python-unittest", TimeoutMinutes: 20}
}

func TestValidatePlan(t *testing.T) {
	if err := ValidatePlan(Plan{Tasks: []Task{validTask("one")}}); err != nil {
		t.Fatal(err)
	}
}
func TestDuplicate(t *testing.T) {
	task := validTask("one")
	if ValidatePlan(Plan{Tasks: []Task{task, task}}) == nil {
		t.Fatal("expected duplicate")
	}
}
func TestCycle(t *testing.T) {
	a, b := validTask("a"), validTask("b")
	a.Dependencies = []string{"b"}
	b.Dependencies = []string{"a"}
	if ValidatePlan(Plan{Tasks: []Task{a, b}}) == nil {
		t.Fatal("expected cycle")
	}
}
func TestOverlap(t *testing.T) {
	a, b := validTask("a"), validTask("b")
	a.AllowedPaths = []string{"src/**"}
	b.AllowedPaths = []string{"src/game/**"}
	if ValidatePlan(Plan{Tasks: []Task{a, b}}) == nil {
		t.Fatal("expected overlap")
	}
}
func TestDependencyAllowsSequentialOwnership(t *testing.T) {
	a, b := validTask("a"), validTask("b")
	a.AllowedPaths = []string{"src/**"}
	b.AllowedPaths = []string{"src/**"}
	b.Dependencies = []string{"a"}
	if err := ValidatePlan(Plan{Tasks: []Task{a, b}}); err != nil {
		t.Fatal(err)
	}
}
func TestProtectedPaths(t *testing.T) {
	for _, path := range []string{".github/**", ".forgegrid/**", "AGENTS.md"} {
		task := validTask("a")
		task.AllowedPaths = []string{path}
		if ValidatePlan(Plan{Tasks: []Task{task}}) == nil {
			t.Fatalf("expected protection for %s", path)
		}
	}
}
