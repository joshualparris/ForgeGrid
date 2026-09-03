package agent

import (
	"context"
	"fmt"
	"os/exec"
)

func init() {
	Register(&CodexProvider{})
}

type CodexProvider struct{}

func (p *CodexProvider) ID() string {
	return "codex"
}

func (p *CodexProvider) DisplayName() string {
	return "OpenAI Codex"
}

func (p *CodexProvider) Detect(ctx context.Context) AgentDetection {
	cmd := exec.CommandContext(ctx, "codex", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return AgentDetection{Available: false, Reason: "executable not found or failed"}
	}
	return AgentDetection{Available: true, Version: string(out), Reason: "codex found in PATH"}
}

func (p *CodexProvider) BuildInvocation(req AgentRequest) (AgentInvocation, error) {
	prompt := BuildCommonPrompt(req)

	// Keep existing arguments matching what was previously executed for Codex
	args := []string{"exec", "--sandbox", "workspace-write"}
	
	// Add prompt. The previous mechanism passed parameters which were appended or passed.
	// If codex expects the prompt as a command-line argument, we do it safely:
	args = append(args, prompt)

	return AgentInvocation{
		Executable: "codex",
		Args:       args,
	}, nil
}

func (p *CodexProvider) InterpretResult(execResult ExecutionResult) AgentResult {
	res := AgentResult{
		Provider: p.ID(),
		ExitCode: execResult.ExitCode,
		Duration: execResult.Duration,
	}

	if execResult.Error != nil {
		if execResult.Error.Error() == "timeout" {
			res.Status = "AGENT_TIMEOUT"
			res.Message = "Codex execution timed out"
		} else if execResult.Error.Error() == "cancelled" {
			res.Status = "AGENT_CANCELLED"
			res.Message = "Codex execution cancelled"
		} else {
			res.Status = "AGENT_FAILED"
			res.Message = fmt.Sprintf("Codex failed: %v", execResult.Error)
		}
		return res
	}

	if execResult.ExitCode == 0 {
		res.Status = "COMPLETED"
		res.Message = "Codex execution successful"
	} else {
		res.Status = "AGENT_FAILED"
		res.Message = fmt.Sprintf("Codex exited with code %d", execResult.ExitCode)
	}

	return res
}
