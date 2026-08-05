package reporting

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	input := "token ghp_" + strings.Repeat("a", 30) + " sk-" + strings.Repeat("b", 30)
	got := Redact(input)
	if strings.Contains(got, "ghp_") || strings.Contains(got, "sk-") {
		t.Fatal(got)
	}
}
