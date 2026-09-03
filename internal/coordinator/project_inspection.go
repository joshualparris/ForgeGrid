package coordinator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"forgegrid/internal/models"
)

type githubRef struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type githubTree struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
	Truncated bool `json:"truncated"`
}

type githubContent struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

func safeForgeGridBranch(actionID, text string, now time.Time) string {
	slug := strings.ToLower(text)
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "work"
	}
	if len(slug) > 42 {
		slug = strings.Trim(slug[:42], "-")
	}
	stamp := now.UTC().Format("20060102-150405")
	action := regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(actionID), "-")
	action = strings.Trim(action, "-")
	if action == "" {
		action = "job"
	}
	return fmt.Sprintf("forgegrid/%s/%s-%s", action, slug, stamp)
}

func (c *Coordinator) inspectProject(ctx context.Context, projectID string, force bool) (*models.ProjectInspection, error) {
	c.Store.Mu.RLock()
	project := c.Store.ProjectLibrary.Projects[projectID]
	if project == nil {
		c.Store.Mu.RUnlock()
		return nil, fmt.Errorf("project not found")
	}
	if !force && project.Inspection != nil && !project.UpdatedAt.After(project.Inspection.InspectionTimestamp) {
		inspection := *project.Inspection
		normalizeInspectionActions(&inspection)
		c.Store.Mu.RUnlock()
		return &inspection, nil
	}
	fullName := project.FullName
	defaultBranch := project.DefaultBranch
	c.Store.Mu.RUnlock()

	token := c.githubToken()
	if token == "" {
		return nil, fmt.Errorf("GitHub not connected")
	}

	client := &http.Client{Timeout: 20 * time.Second}
	sha, err := githubDefaultSHA(ctx, client, token, fullName, defaultBranch)
	if err != nil {
		return nil, err
	}
	paths, truncated, err := githubTreePaths(ctx, client, token, fullName, sha)
	if err != nil {
		return nil, err
	}
	packageScripts := map[string]string{}
	if hasPath(paths, "package.json") {
		packageScripts = githubPackageScripts(ctx, client, token, fullName, defaultBranch)
	}
	inspection := buildInspection(projectID, defaultBranch, sha, paths, packageScripts, truncated)
	normalizeInspectionActions(inspection)

	c.Store.Mu.Lock()
	if stored := c.Store.ProjectLibrary.Projects[projectID]; stored != nil {
		stored.DefaultSHA = sha
		stored.Inspection = inspection
	}
	if err := c.Store.Save(); err != nil {
		c.Store.Mu.Unlock()
		return nil, err
	}
	c.Store.Mu.Unlock()
	return inspection, nil
}

func normalizeInspectionActions(inspection *models.ProjectInspection) {
	if inspection == nil {
		return
	}
	for i := range inspection.AvailableActions {
		action := &inspection.AvailableActions[i]
		if action.ID == "codex" || action.Profile == "CodexExec" && action.Label == "Work on with Codex" {
			action.ID = "ai-agent"
			action.Label = "Ask AI To Work On This Project"
			action.Description = "Describe a coding task in plain English"
			action.Profile = "AIAgentAuto"
			action.RequiredCapabilities = []string{"ai-agent"}
		}
	}
}

func githubDefaultSHA(ctx context.Context, client *http.Client, token, fullName, branch string) (string, error) {
	var ref githubRef
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/git/ref/heads/%s", fullName, branch)
	if err := githubGet(ctx, client, token, apiURL, &ref); err != nil {
		return "", err
	}
	return ref.Object.SHA, nil
}

func githubTreePaths(ctx context.Context, client *http.Client, token, fullName, sha string) ([]string, bool, error) {
	var tree githubTree
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/git/trees/%s?recursive=1", fullName, sha)
	if err := githubGet(ctx, client, token, apiURL, &tree); err != nil {
		return nil, false, err
	}
	paths := make([]string, 0, len(tree.Tree))
	for _, entry := range tree.Tree {
		if entry.Type == "blob" {
			paths = append(paths, entry.Path)
		}
	}
	sort.Strings(paths)
	return paths, tree.Truncated, nil
}

func githubPackageScripts(ctx context.Context, client *http.Client, token, fullName, branch string) map[string]string {
	var content githubContent
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/package.json?ref=%s", fullName, branch)
	if err := githubGet(ctx, client, token, apiURL, &content); err != nil {
		return nil
	}
	if content.Encoding != "base64" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil
	}
	return pkg.Scripts
}

func buildInspection(projectID, branch, sha string, paths []string, scripts map[string]string, truncated bool) *models.ProjectInspection {
	pathSet := make(map[string]bool, len(paths))
	for _, path := range paths {
		pathSet[path] = true
	}
	var types, langs, detected, warnings []string
	var actions []models.ProjectAction
	addDetected := func(path string) {
		if pathSet[path] {
			detected = append(detected, path)
		}
	}
	if pathSet["go.mod"] {
		types = append(types, "Go")
		langs = append(langs, "Go")
		detected = append(detected, "go.mod")
		actions = append(actions,
			action("go-test", "Test Project", "Run go test ./...", "GoTest", map[string]string{"package": "./..."}, []string{"go"}, "", false, 600),
			action("go-race", "Race Test", "Run go test -race ./...", "GoRaceTest", map[string]string{"package": "./..."}, []string{"go"}, "", false, 900),
			action("go-build", "Build Project", "Run go build ./...", "GoBuild", map[string]string{"package": "./..."}, []string{"go"}, "", false, 600),
		)
	}
	python := hasAnyPath(pathSet, "pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "pytest.ini", "tox.ini", "Pipfile") || hasExtension(paths, ".py")
	if python {
		types = append(types, "Python")
		langs = append(langs, "Python")
		for _, p := range []string{"pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "pytest.ini", "tox.ini", "Pipfile"} {
			addDetected(p)
		}
		actions = append(actions, action("python-check", "Python Syntax Check", "Compile Python files without running the app", "PythonCompile", map[string]string{"path": "."}, []string{"python"}, "", false, 300))
		if hasAnyPath(pathSet, "pytest.ini", "tox.ini") || hasPrefix(paths, "tests/") {
			actions = append(actions, action("python-test", "Test Project", "Run unittest discovery", "PythonUnittest", map[string]string{"module": "discover"}, []string{"python"}, "", false, 300))
		} else {
			warnings = append(warnings, "No automated Python test suite detected")
		}
	}
	if pathSet["package.json"] {
		types = append(types, "Node")
		langs = append(langs, "JavaScript")
		detected = append(detected, "package.json")
		pm := "npm"
		if pathSet["pnpm-lock.yaml"] {
			pm = "pnpm"
			detected = append(detected, "pnpm-lock.yaml")
		} else if pathSet["yarn.lock"] {
			pm = "yarn"
			detected = append(detected, "yarn.lock")
		} else if pathSet["package-lock.json"] {
			detected = append(detected, "package-lock.json")
		}
		for _, name := range []string{"test", "build", "lint"} {
			if _, ok := scripts[name]; ok && pm == "npm" {
				profile := map[string]string{"test": "NodeTest", "build": "NodeBuild", "lint": "NodeLint"}[name]
				actions = append(actions, action("node-"+name, strings.Title(name)+" Project", "Run npm "+name, profile, nil, []string{"node"}, "", false, 600))
			}
		}
		if pm != "npm" {
			warnings = append(warnings, "Detected "+pm+" project; npm-only execution profiles are currently available")
		}
	}
	if pathSet["project.godot"] {
		types = append(types, "Godot")
		detected = append(detected, "project.godot")
		actions = append(actions, action("godot-export-windows", "Export Windows Build", "Run Godot headless Windows export", "GodotExport", map[string]string{"preset": "Windows Desktop", "export_path": "build/game.exe"}, []string{"godot"}, "windows", false, 1800))
	}
	if hasExtension(paths, ".sln") || hasExtension(paths, ".csproj") || hasExtension(paths, ".fsproj") {
		types = append(types, ".NET")
		langs = append(langs, "C#")
		warnings = append(warnings, ".NET project detected; execution profile is not configured yet")
	}
	if pathSet["Cargo.toml"] {
		types = append(types, "Rust")
		langs = append(langs, "Rust")
		detected = append(detected, "Cargo.toml")
		warnings = append(warnings, "Rust project detected; execution profile is not configured yet")
	}
	if truncated {
		warnings = append(warnings, "GitHub tree was truncated; inspection may be incomplete")
	}
	actions = append([]models.ProjectAction{
		action("ai-agent", "Ask AI To Work On This Project", "Describe a coding task in plain English", "AIAgentAuto", nil, []string{"ai-agent"}, "", true, 3600),
		action("inspect", "Inspect Project", "Refresh project detection", "", nil, nil, "", false, 0),
	}, actions...)
	return &models.ProjectInspection{
		ProjectID:           projectID,
		DefaultBranch:       branch,
		DefaultSHA:          sha,
		Languages:           unique(langs),
		ProjectTypes:        unique(types),
		DetectedFiles:       unique(detected),
		AvailableActions:    actions,
		Warnings:            warnings,
		InspectionTimestamp: time.Now(),
		InspectionSource:    "github",
	}
}

func action(id, label, desc, profile string, params map[string]string, caps []string, os string, commit bool, timeout int) models.ProjectAction {
	return models.ProjectAction{ID: id, Label: label, Description: desc, Profile: profile, Parameters: params, RequiredCapabilities: caps, RequiredOS: os, CommitChanges: commit, TimeoutSeconds: timeout}
}

func hasPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func hasAnyPath(pathSet map[string]bool, paths ...string) bool {
	for _, path := range paths {
		if pathSet[path] {
			return true
		}
	}
	return false
}

func hasPrefix(paths []string, prefix string) bool {
	for _, path := range paths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func hasExtension(paths []string, ext string) bool {
	for _, path := range paths {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
