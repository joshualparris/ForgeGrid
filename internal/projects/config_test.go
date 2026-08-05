package projects

import (
	"strings"
	"testing"
)

const validConfig = `{"schema_version":1,"project_id":"adventuretext","repository":"joshparri/AdventureText","default_branch":"main","visibility_required":"private","execution_profile":"codex-safe","runner_labels":["self-hosted","forgegrid"],"test_profile":"python-unittest","test_arguments":["discover","-s","tests","-v"],"environment":{"PYTHONDONTWRITEBYTECODE":"1"},"locked_paths":[".github/**"],"task_branch_prefix":"task/","draft_pull_requests":true,"automatic_merge":false}`

func TestDecodeValid(t *testing.T) {
	if _, err := Decode(strings.NewReader(validConfig)); err != nil {
		t.Fatal(err)
	}
}
func TestRejectsUnknownFields(t *testing.T) {
	_, err := Decode(strings.NewReader(strings.TrimSuffix(validConfig, "}") + `,"command":"cmd /c whoami"}`))
	if err == nil {
		t.Fatal("expected unknown field rejection")
	}
}
func TestRejectsPATHOverride(t *testing.T) {
	bad := strings.Replace(validConfig, `"PYTHONDONTWRITEBYTECODE":"1"`, `"PATH":"unsafe"`, 1)
	if _, err := Decode(strings.NewReader(bad)); err == nil {
		t.Fatal("expected PATH rejection")
	}
}
func TestRejectsPublicOrAutomaticMerge(t *testing.T) {
	for _, bad := range []string{strings.Replace(validConfig, `"private"`, `"public"`, 1), strings.Replace(validConfig, `"automatic_merge":false`, `"automatic_merge":true`, 1)} {
		if _, err := Decode(strings.NewReader(bad)); err == nil {
			t.Fatal("expected rejection")
		}
	}
}
