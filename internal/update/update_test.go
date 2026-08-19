package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateManifestSelectsWorkerArtifact(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Windows", "ForgeGrid.exe"), []byte("windows"))
	writeFile(t, filepath.Join(dir, "Linux", "forgegrid"), []byte("linux"))

	m := &Manifest{
		SchemaVersion: "1",
		Product:       "ForgeGrid",
		Version:       "0.9.0",
		Protocol:      "1",
		Artifacts: []Artifact{
			{Role: "worker", Platform: "windows", Architecture: "amd64", SHA256: sum([]byte("windows")), Path: "Windows/ForgeGrid.exe"},
			{Role: "worker", Platform: "linux", Architecture: "amd64", SHA256: sum([]byte("linux")), Path: "Linux/forgegrid"},
		},
	}
	if err := ValidateManifest(m, dir); err != nil {
		t.Fatalf("validate manifest: %v", err)
	}
	artifact, ok := SelectArtifact(m, "worker", "windows", "amd64")
	if !ok {
		t.Fatalf("expected windows worker artifact")
	}
	if artifact.Path != "Windows/ForgeGrid.exe" {
		t.Fatalf("artifact path = %q", artifact.Path)
	}
}

func TestValidateManifestRejectsUnsafeArtifacts(t *testing.T) {
	tests := []struct {
		name     string
		artifact Artifact
	}{
		{"path traversal", Artifact{Role: "worker", Platform: "linux", Architecture: "amd64", SHA256: sum([]byte("x")), Path: "../forgegrid"}},
		{"bad checksum", Artifact{Role: "worker", Platform: "linux", Architecture: "amd64", SHA256: "abc", Path: "Linux/forgegrid"}},
		{"bad url", Artifact{Role: "worker", Platform: "linux", Architecture: "amd64", SHA256: sum([]byte("x")), URL: "http://example.test/forgegrid"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{SchemaVersion: "1", Product: "ForgeGrid", Version: "0.9.0", Artifacts: []Artifact{tc.artifact}}
			if err := ValidateManifest(m, ""); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestVerifyFileChecksSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "forgegrid")
	content := []byte("release")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(path, sum(content)); err != nil {
		t.Fatalf("verify good file: %v", err)
	}
	if err := VerifyFile(path, sum([]byte("other"))); err == nil {
		t.Fatalf("expected checksum mismatch")
	}
}

func TestNeedsUpdateTreatsUnversionedWorkerAsOld(t *testing.T) {
	if !NeedsUpdate("", "0.9.0") {
		t.Fatalf("unversioned worker should need update when a latest version exists")
	}
	if NeedsUpdate("0.9.0", "0.9.0") {
		t.Fatalf("matching worker should not need update")
	}
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func sum(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}
