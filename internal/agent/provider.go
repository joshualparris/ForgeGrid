package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type AgentDetection struct {
	Available bool
	Version   string
	Reason    string
}

type AgentRequest struct {
	Task                string
	Repository          string
	ProjectName         string
	ProjectTechnologies []string
	Workspace           string
	BaseBranch          string
	BaseSHA             string
	WorkBranch          string
	ValidationPlan      string
	SafetyInstructions  string
}

type AgentInvocation struct {
	Executable string
	Args       []string
	Env        []string
	Stdin      []byte
}

type ExecutionResult struct {
	ExitCode int
	Duration time.Duration
	Stdout   []byte
	Stderr   []byte
	Error    error
}

type AgentResult struct {
	Provider string
	ExitCode int
	Duration time.Duration
	Status   string
	Message  string
}

type AgentProvider interface {
	ID() string
	DisplayName() string
	Detect(ctx context.Context) AgentDetection
	BuildInvocation(req AgentRequest) (AgentInvocation, error)
	InterpretResult(exec ExecutionResult) AgentResult
}

var registry = make(map[string]AgentProvider)

func Register(p AgentProvider) {
	registry[p.ID()] = p
}

func GetProvider(id string) (AgentProvider, error) {
	if p, ok := registry[id]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("agent provider not found: %s", id)
}

func RegisteredProviders() []AgentProvider {
	var list []AgentProvider
	for _, p := range registry {
		list = append(list, p)
	}
	return list
}

func StandardSafetyInstructions() string {
	return `You are operating inside a ForgeGrid-managed isolated Git workspace.
Work only inside the supplied repository.
Inspect the existing project before modifying it.
Make only changes relevant to the user's request.
Do not modify files outside the supplied repository.
Do not push changes.
Do not force-push or rewrite repository history.
Do not edit main/master directly.
ForgeGrid owns validation, secret scanning, commits and optional branch pushing after you finish.
Do not access unrelated machine credentials.
`
}

func BuildCommonPrompt(req AgentRequest) string {
	var sb strings.Builder
	sb.WriteString(req.SafetyInstructions)
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("Project: %s\n", req.ProjectName))
	if len(req.ProjectTechnologies) > 0 {
		sb.WriteString(fmt.Sprintf("Technologies: %s\n", strings.Join(req.ProjectTechnologies, ", ")))
	}
	sb.WriteString(fmt.Sprintf("Workspace Directory: %s\n", req.Workspace))
	sb.WriteString(fmt.Sprintf("Base Branch: %s\n", req.BaseBranch))
	sb.WriteString(fmt.Sprintf("Work Branch: %s\n", req.WorkBranch))
	sb.WriteString("\n---\n\n")
	sb.WriteString("USER TASK:\n")
	sb.WriteString(req.Task)
	sb.WriteString("\n")
	return sb.String()
}
