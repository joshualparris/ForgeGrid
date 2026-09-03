package gitworkspace

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"forgegrid/internal/models"
)

// Manager handles restricted git operations to prepare isolated worktrees
type Manager struct {
	BaseDir      string
	AllowedRepos map[string]bool
	AllowPush    bool
}

type Options struct {
	AllowedRepos map[string]bool
	AllowPush    bool
}

type Workspace struct {
	ID         string
	RootDir    string
	RepoDir    string
	WorkDir    string
	BranchName string
	BaseCommit string
}

type CommitResult struct {
	Message   string
	CommitSHA string
	Pushed    bool
}

// NewManager creates a new Git workspace manager
func NewManager(baseDir string, opts ...Options) *Manager {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	return &Manager{
		BaseDir:      baseDir,
		AllowedRepos: options.AllowedRepos,
		AllowPush:    options.AllowPush,
	}
}

// PrepareWorkspace clones (if needed), fetches, and creates an isolated worktree
func (m *Manager) PrepareWorkspace(repoURL, pinnedBaseCommit, generatedBranchName, taskID string) (string, error) {
	ws, err := m.PrepareJobWorkspace(repoURL, pinnedBaseCommit, generatedBranchName, taskID)
	if err != nil {
		return "", err
	}
	return ws.WorkDir, nil
}

func (m *Manager) PrepareJobWorkspace(repoURL, pinnedBaseCommit, generatedBranchName, taskID string) (*Workspace, error) {
	if len(m.AllowedRepos) == 0 || !m.AllowedRepos[repoURL] {
		return nil, fmt.Errorf("repository not in allowlist: %s", repoURL)
	}
	if strings.TrimSpace(pinnedBaseCommit) == "" {
		return nil, fmt.Errorf("base commit is required for git workspace jobs")
	}
	if err := ValidateBranchName(generatedBranchName); err != nil {
		return nil, err
	}

	workspaceID := safeName(taskID)
	if workspaceID == "" {
		workspaceID = "job"
	}
	repoName := safeName(strings.TrimSuffix(filepath.Base(repoURL), ".git"))
	if repoName == "" {
		repoName = "repo"
	}
	jobRoot := filepath.Join(m.BaseDir, "workspaces", workspaceID)
	repoDir := filepath.Join(jobRoot, repoName)

	if err := ensureContained(m.BaseDir, jobRoot); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(jobRoot); err != nil {
		return nil, fmt.Errorf("failed to reset job workspace: %w", err)
	}
	if err := os.MkdirAll(jobRoot, 0700); err != nil {
		return nil, fmt.Errorf("failed to create job workspace: %w", err)
	}
	if err := m.runGit(jobRoot, "clone", repoURL, repoName); err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	if err := m.runGit(repoDir, "fetch", "origin", "--prune"); err != nil {
		return nil, fmt.Errorf("failed to fetch remote: %w", err)
	}
	if err := m.runGit(repoDir, "cat-file", "-e", pinnedBaseCommit+"^{commit}"); err != nil {
		return nil, fmt.Errorf("pinned base commit %s not found or invalid: %w", pinnedBaseCommit, err)
	}
	if err := m.runGit(repoDir, "ls-remote", "--exit-code", "--heads", "origin", generatedBranchName); err == nil {
		return nil, fmt.Errorf("branch already exists on origin: %s", generatedBranchName)
	}
	if err := m.runGit(repoDir, "checkout", "-b", generatedBranchName, pinnedBaseCommit); err != nil {
		return nil, fmt.Errorf("failed to create work branch: %w", err)
	}

	resolvedBase, err := m.runGitWithOutput(repoDir, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base commit: %w", err)
	}
	return &Workspace{
		ID:         workspaceID,
		RootDir:    jobRoot,
		RepoDir:    repoDir,
		WorkDir:    repoDir,
		BranchName: generatedBranchName,
		BaseCommit: strings.TrimSpace(resolvedBase),
	}, nil
}

// CleanWorktree checks if the worktree is clean
func (m *Manager) IsWorktreeClean(worktreeDir string) (bool, error) {
	out, err := m.runGitWithOutput(worktreeDir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("failed to check worktree status: %w", err)
	}
	return len(strings.TrimSpace(out)) == 0, nil
}

// ProduceDiff produces a diff and status report
func (m *Manager) ProduceDiff(worktreeDir string) (string, error) {
	status, err := m.runGitWithOutput(worktreeDir, "status")
	if err != nil {
		return "", fmt.Errorf("failed to get status: %w", err)
	}
	diff, err := m.runGitWithOutput(worktreeDir, "diff")
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w", err)
	}
	return fmt.Sprintf("STATUS:\n%s\n\nDIFF:\n%s", status, diff), nil
}

// CleanupWorktree removes the worktree safely
func (m *Manager) CleanupWorktree(mainRepoDir, worktreeDir, branchName string) error {
	if strings.Contains(filepath.Clean(worktreeDir), string(filepath.Separator)+"workspaces"+string(filepath.Separator)) {
		return os.RemoveAll(filepath.Dir(worktreeDir))
	}
	// First remove the worktree
	if err := m.runGit(mainRepoDir, "worktree", "remove", "--force", worktreeDir); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}
	// Delete the branch
	if err := m.runGit(mainRepoDir, "branch", "-D", branchName); err != nil {
		return fmt.Errorf("failed to delete worktree branch: %w", err)
	}
	return nil
}

// CommitAndMaybePush commits all changes in the worktree and optionally pushes to origin.
func (m *Manager) CommitAndMaybePush(worktreeDir, repoURL, commitMsg string, push bool) (string, error) {
	result, err := m.CommitAndMaybePushDetailed(worktreeDir, repoURL, commitMsg, push)
	if result == nil {
		return "", err
	}
	return result.Message, err
}

func (m *Manager) CommitAndMaybePushDetailed(worktreeDir, repoURL, commitMsg string, push bool) (*CommitResult, error) {
	if push {
		if !m.AllowPush {
			return nil, fmt.Errorf("push denied by worker policy")
		}
		if len(m.AllowedRepos) == 0 || !m.AllowedRepos[repoURL] {
			return nil, fmt.Errorf("push denied because repository is not in allowlist: %s", repoURL)
		}
	}

	if err := m.runGit(worktreeDir, "add", "."); err != nil {
		return nil, fmt.Errorf("failed to add changes: %w", err)
	}

	status, err := m.runGitWithOutput(worktreeDir, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to check status before commit: %w", err)
	}
	if len(strings.TrimSpace(status)) == 0 {
		return &CommitResult{Message: "No changes to commit."}, nil
	}

	if strings.TrimSpace(commitMsg) == "" {
		return nil, fmt.Errorf("commit message is required")
	}
	if err := m.runGit(worktreeDir, "commit", "-m", commitMsg); err != nil {
		return nil, fmt.Errorf("failed to commit changes: %w", err)
	}
	commitSHA, err := m.runGitWithOutput(worktreeDir, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to read commit sha: %w", err)
	}

	if !push {
		return &CommitResult{Message: "Committed changes locally; push disabled for this job.", CommitSHA: strings.TrimSpace(commitSHA)}, nil
	}
	if err := m.runGit(worktreeDir, "push", "origin", "HEAD"); err != nil {
		return nil, fmt.Errorf("failed to push changes: %w", err)
	}

	return &CommitResult{Message: "Committed and pushed changes to origin.", CommitSHA: strings.TrimSpace(commitSHA), Pushed: true}, nil
}

func ValidateBranchName(branchName string) error {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return fmt.Errorf("branch name is required")
	}
	if branchName == "main" || branchName == "master" || branchName == "develop" {
		return fmt.Errorf("refusing to use protected branch name: %s", branchName)
	}
	if strings.Contains(branchName, "\\") || strings.Contains(branchName, "..") || strings.HasPrefix(branchName, "/") || strings.HasSuffix(branchName, "/") {
		return fmt.Errorf("unsafe branch name: %s", branchName)
	}
	for _, r := range branchName {
		if r < 32 || r == 127 {
			return fmt.Errorf("unsafe branch name contains control characters")
		}
	}
	cmd := exec.Command("git", "check-ref-format", "--branch", branchName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("invalid branch name %q: %s", branchName, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) ChangedFiles(worktreeDir string) ([]models.ChangedFile, error) {
	out, err := m.runGitWithOutput(worktreeDir, "status", "--porcelain=v1", "-z")
	if err != nil {
		return nil, fmt.Errorf("failed to read changed files: %w", err)
	}
	return ParseChangedFiles(out), nil
}

func ParseChangedFiles(raw string) []models.ChangedFile {
	parts := strings.Split(raw, "\x00")
	files := make([]models.ChangedFile, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if entry == "" || len(entry) < 4 {
			continue
		}
		status := strings.TrimSpace(entry[:2])
		path := strings.TrimSpace(entry[3:])
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if i+1 < len(parts) && parts[i+1] != "" {
				path = path + " -> " + strings.TrimSpace(parts[i+1])
				i++
			}
		}
		files = append(files, models.ChangedFile{Path: path, Status: status})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func SecretLikeChangedFiles(files []models.ChangedFile) []string {
	var blocked []string
	for _, file := range files {
		for _, p := range strings.Split(file.Path, " -> ") {
			if secretLikePath(p) {
				blocked = append(blocked, p)
			}
		}
	}
	sort.Strings(blocked)
	return blocked
}

func (m *Manager) CreatePullRequest(worktreeDir, title, body string) (string, error) {
	if strings.TrimSpace(title) == "" {
		title = "ForgeGrid automated changes"
	}
	out, err := m.runWithOutput(worktreeDir, "gh", "pr", "create", "--title", title, "--body", body)
	if err != nil {
		return "", fmt.Errorf("failed to create pull request: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (m *Manager) CollectArtifacts(worktreeDir string, patterns []string) ([]Artifact, error) {
	var artifacts []Artifact
	seen := make(map[string]bool)
	for _, pattern := range patterns {
		matches, err := artifactMatches(worktreeDir, pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid artifact pattern %s: %w", pattern, err)
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			rel, err := filepath.Rel(worktreeDir, match)
			if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
				continue
			}
			if seen[rel] {
				continue
			}
			sum, err := fileSHA256(match)
			if err != nil {
				return nil, err
			}
			artifact := Artifact{Path: rel, Size: info.Size(), SHA256: sum}
			if err := attachArtifactContent(&artifact, match); err != nil {
				return nil, err
			}
			artifacts = append(artifacts, artifact)
			seen[rel] = true
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func artifactMatches(root, pattern string) ([]string, error) {
	clean := filepath.Clean(pattern)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("artifact pattern escapes workspace")
	}
	if strings.HasSuffix(pattern, "/**") || strings.HasSuffix(pattern, string(filepath.Separator)+"**") {
		dir := strings.TrimSuffix(strings.TrimSuffix(pattern, "/**"), string(filepath.Separator)+"**")
		base := filepath.Join(root, filepath.Clean(dir))
		var matches []string
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				matches = append(matches, path)
			}
			return nil
		})
		if os.IsNotExist(err) {
			return nil, nil
		}
		return matches, err
	}
	return filepath.Glob(filepath.Join(root, clean))
}

func attachArtifactContent(artifact *Artifact, path string) error {
	if artifact.Size <= 10*1024*1024 {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		artifact.ContentBase64 = base64.StdEncoding.EncodeToString(b)
		return nil
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(filepath.Base(path))
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		zw.Close()
		return err
	}
	_, copyErr := io.Copy(w, f)
	closeErr := f.Close()
	if copyErr != nil {
		zw.Close()
		return copyErr
	}
	if closeErr != nil {
		zw.Close()
		return closeErr
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if buf.Len() <= 10*1024*1024 {
		artifact.ContentBase64 = base64.StdEncoding.EncodeToString(buf.Bytes())
		artifact.Packaged = true
		artifact.PackageName = filepath.Base(path) + ".zip"
	}
	return nil
}

type Artifact struct {
	Path          string
	Size          int64
	SHA256        string
	ContentBase64 string
	Packaged      bool
	PackageName   string
}

// Internal restricted git command execution
func (m *Manager) runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func (m *Manager) runGitWithOutput(dir string, args ...string) (string, error) {
	return m.runWithOutput(dir, "git", args...)
}

func (m *Manager) runWithOutput(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && stderr.Len() > 0 {
		return out.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func safeName(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-_")
}

func ensureContained(baseDir, path string) error {
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return err
	}
	if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
		return nil
	}
	return fmt.Errorf("workspace path escapes base directory")
}

func secretLikePath(path string) bool {
	clean := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	base := filepath.Base(clean)
	if clean == "" {
		return false
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	switch base {
	case "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "github-token.txt", "worker_creds.json", "worker_policy.json", "coordinator.json":
		return true
	}
	if strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx") {
		return true
	}
	return strings.Contains(clean, "/.ssh/") || strings.Contains(clean, "/secrets/")
}
