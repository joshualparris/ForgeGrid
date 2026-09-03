package agent

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func init() {
	Register(&AntigravityProvider{})
}

type AntigravityProvider struct{}

func (p *AntigravityProvider) ID() string {
	return "antigravity"
}

func (p *AntigravityProvider) DisplayName() string {
	return "Google Antigravity"
}

func (p *AntigravityProvider) Detect(ctx context.Context) AgentDetection {
	// Antigravity executable might be "agy" on Linux/macOS or "agy.exe" / "agy-node.cmd" / "agentapi.bat" on Windows.
	exeName := "agy"
	if runtime.GOOS == "windows" {
		// Just check if it's in PATH, or try a few common aliases
		if _, err := exec.LookPath("agy"); err == nil {
			exeName = "agy"
		} else if _, err := exec.LookPath("agentapi"); err == nil {
			exeName = "agentapi"
		} else if _, err := exec.LookPath("agy-node"); err == nil {
			exeName = "agy-node"
		} else {
			return AgentDetection{Available: false, Reason: "no Antigravity executable found in PATH"}
		}
	}

	cmd := exec.CommandContext(ctx, exeName, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return AgentDetection{Available: false, Reason: fmt.Sprintf("%s failed to execute", exeName)}
	}
	return AgentDetection{Available: true, Version: strings.TrimSpace(string(out)), Reason: fmt.Sprintf("%s found", exeName)}
}

func (p *AntigravityProvider) BuildInvocation(req AgentRequest) (AgentInvocation, error) {
	prompt := BuildCommonPrompt(req)

	exeName := "agy"
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("agy"); err == nil {
			exeName = "agy"
		} else if _, err := exec.LookPath("agentapi"); err == nil {
			exeName = "agentapi"
		} else if _, err := exec.LookPath("agy-node"); err == nil {
			exeName = "agy-node"
		}
	}

	args := []string{
		"--dangerously-skip-permissions", // Required to let the agent work without interactive prompts
		"--print", prompt,                // Run non-interactively
	}

	return AgentInvocation{
		Executable: exeName,
		Args:       args,
	}, nil
}

func (p *AntigravityProvider) InterpretResult(execResult ExecutionResult) AgentResult {
	res := AgentResult{
		Provider: p.ID(),
		ExitCode: execResult.ExitCode,
		Duration: execResult.Duration,
	}

	if execResult.Error != nil {
		if execResult.Error.Error() == "timeout" {
			res.Status = "AGENT_TIMEOUT"
			res.Message = "Antigravity execution timed out"
		} else if execResult.Error.Error() == "cancelled" {
			res.Status = "AGENT_CANCELLED"
			res.Message = "Antigravity execution cancelled"
		} else {
			res.Status = "AGENT_FAILED"
			res.Message = fmt.Sprintf("Antigravity failed: %v", execResult.Error)
		}
		return res
	}

	if execResult.ExitCode == 0 {
		res.Status = "COMPLETED"
		res.Message = "Antigravity execution successful"
	} else {
		res.Status = "AGENT_FAILED"
		res.Message = fmt.Sprintf("Antigravity exited with code %d", execResult.ExitCode)
	}

	return res
}
