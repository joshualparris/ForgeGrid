package attempts

import (
	"errors"
	"testing"
)

func TestAtomicClaimRejectsDuplicate(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim("help-command-001", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim("help-command-001", 1); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	if _, err = store.Claim("help-command-001", 2); err != nil {
		t.Fatalf("new attempt should work: %v", err)
	}
}
