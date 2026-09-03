package agent

import (
	"context"
	"fmt"
	"strings"
)

func init() {
	Register(&FakeProvider{})
}

type FakeProvider struct{}

func (p *FakeProvider) ID() string {
	return "fake"
}

func (p *FakeProvider) DisplayName() string {
	return "Fake Agent"
}

func (p *FakeProvider) Detect(ctx context.Context) AgentDetection {
	return AgentDetection{Available: true, Version: "1.0", Reason: "fake is always available"}
}

func (p *FakeProvider) BuildInvocation(req AgentRequest) (AgentInvocation, error) {
	// For testing, we just use a small bash script to simulate success or failure based on the task name
	script := "exit 0"
	if strings.Contains(req.Task, "FAIL") {
		script = "exit 1"
	} else if strings.Contains(req.Task, "TIMEOUT") {
		script = "sleep 10; exit 0"
	} else if strings.Contains(req.Task, "CHANGE") {
		script = "echo 'changed' > changed.txt; exit 0"
	}

	return AgentInvocation{
		Executable: "bash",
		Args:       []string{"-c", script},
	}, nil
}

func (p *FakeProvider) InterpretResult(execResult ExecutionResult) AgentResult {
	res := AgentResult{
		Provider: p.ID(),
		ExitCode: execResult.ExitCode,
		Duration: execResult.Duration,
	}

	if execResult.Error != nil {
		if execResult.Error.Error() == "timeout" {
			res.Status = "AGENT_TIMEOUT"
			res.Message = "Fake execution timed out"
		} else if execResult.Error.Error() == "cancelled" {
			res.Status = "AGENT_CANCELLED"
			res.Message = "Fake execution cancelled"
		} else {
			res.Status = "AGENT_FAILED"
			res.Message = fmt.Sprintf("Fake failed: %v", execResult.Error)
		}
		return res
	}

	if execResult.ExitCode == 0 {
		res.Status = "COMPLETED"
		res.Message = "Fake execution successful"
	} else {
		res.Status = "AGENT_FAILED"
		res.Message = fmt.Sprintf("Fake exited with code %d", execResult.ExitCode)
	}

	return res
}
