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

func TestSelectArtifactIsArchitectureSpecific(t *testing.T) {
	m := &Manifest{
		SchemaVersion: "1",
		Product:       "ForgeGrid",
		Version:       "0.9.0",
		Artifacts: []Artifact{
			{Role: "worker", Platform: "windows", Architecture: "amd64", SHA256: sum([]byte("amd64")), Path: "x"},
			{Role: "worker", Platform: "windows", Architecture: "386", SHA256: sum([]byte("386")), Path: "y"},
		},
	}

	amd64Artifact, ok := SelectArtifact(m, "worker", "windows", "amd64")
	if !ok || amd64Artifact.SHA256 != sum([]byte("amd64")) {
		t.Fatalf("expected amd64 artifact, got %+v ok=%v", amd64Artifact, ok)
	}
	x86Artifact, ok := SelectArtifact(m, "worker", "windows", "386")
	if !ok || x86Artifact.SHA256 != sum([]byte("386")) {
		t.Fatalf("expected 386 artifact, got %+v ok=%v", x86Artifact, ok)
	}
	if _, ok := SelectArtifact(m, "worker", "windows", "arm64"); ok {
		t.Fatalf("expected no artifact for unsupported architecture arm64")
	}
}

func TestValidateManifestRejectsDuplicateArtifacts(t *testing.T) {
	m := &Manifest{
		SchemaVersion: "1",
		Product:       "ForgeGrid",
		Version:       "0.9.0",
		Artifacts: []Artifact{
			{Role: "worker", Platform: "windows", Architecture: "amd64", SHA256: sum([]byte("a")), URL: "file:///a"},
			{Role: "worker", Platform: "windows", Architecture: "amd64", SHA256: sum([]byte("b")), URL: "file:///b"},
		},
	}
	if err := ValidateManifest(m, ""); err == nil {
		t.Fatalf("expected duplicate artifact to be rejected")
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
	if !NeedsUpdate("", "", "0.9.0", "commit-b") {
		t.Fatalf("unversioned worker should need update when a latest version exists")
	}
	if NeedsUpdate("0.9.0", "commit-a", "0.9.0", "commit-a") {
		t.Fatalf("matching version and commit should not need update")
	}
}

func TestNeedsUpdateComparesCommitWhenVersionIsUnchanged(t *testing.T) {
	// This is the real-world case this function exists to catch: several
	// ForgeGrid commits in a row have all shipped under version "0.8.0".
	if !NeedsUpdate("0.8.0", "commit-old", "0.8.0", "commit-new") {
		t.Fatalf("same version but a different commit must still be reported as needing update")
	}
	if NeedsUpdate("0.8.0", "commit-new", "0.8.0", "commit-new") {
		t.Fatalf("same version and same commit must be reported as current")
	}
}

func TestNeedsUpdateFailsConservativelyOnMissingCommitMetadata(t *testing.T) {
	if !NeedsUpdate("0.8.0", "", "0.8.0", "commit-new") {
		t.Fatalf("worker with unknown commit must not be assumed current just because the version matches")
	}
	if !NeedsUpdate("0.8.0", "commit-old", "0.8.0", "") {
		t.Fatalf("manifest with unknown commit must not be assumed to match the worker's commit")
	}
}

func TestNeedsUpdateStillHonoursVersionDifferences(t *testing.T) {
	if !NeedsUpdate("0.7.0", "commit-a", "0.9.0", "commit-a") {
		t.Fatalf("a differing semantic version must still be reported as needing update even with a matching commit string")
	}
	if !NeedsUpdate("0.9.0", "commit-a", "0.8.0", "commit-a") {
		t.Fatalf("an older manifest version relative to the worker must still report needing update (version is authoritative here, not assumed downgrade-safe)")
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
