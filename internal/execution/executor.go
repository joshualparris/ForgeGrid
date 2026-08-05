package execution

import (
	"bufio"
	"context"
	"os/exec"
	"sync"
)

type ExecutionResult struct {
	ExitCode int
	Error    error
	Logs     []string
}

type Executor struct {
	logLimit int
}

func NewExecutor() *Executor {
	return &Executor{logLimit: 2000}
}

func (e *Executor) Run(ctx context.Context, profile Profile, args []string, env map[string]string, workDir string) ExecutionResult {
	cmd := exec.CommandContext(ctx, profile.Executable, args...)
	cmd.Dir = workDir

	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	setupCmd(cmd)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	var mu sync.Mutex
	logs := make([]string, 0)

	addLog := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		if len(logs) < e.logLimit {
			logs = append(logs, line)
		} else if len(logs) == e.logLimit {
			logs = append(logs, "... [logs truncated]")
		}
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			addLog(scanner.Text())
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			addLog(scanner.Text())
		}
	}()

	err := cmd.Start()
	if err != nil {
		return ExecutionResult{ExitCode: -1, Error: err, Logs: logs}
	}

	cleanup := manageProcess(ctx, cmd)
	defer cleanup()

	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()

	mu.Lock()
	defer mu.Unlock()
	return ExecutionResult{ExitCode: exitCode, Error: err, Logs: logs}
}
