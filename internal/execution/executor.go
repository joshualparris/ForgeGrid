package execution

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Executor interface {
	Execute(ctx context.Context, profile Profile, params map[string]string, workDir string) ([]byte, error)
}

type DefaultExecutor struct{}

func NewExecutor() Executor {
	return &DefaultExecutor{}
}

func (e *DefaultExecutor) Execute(ctx context.Context, profile Profile, params map[string]string, workDir string) ([]byte, error) {
	args, err := BuildArgs(profile, params)
	if err != nil {
		return nil, fmt.Errorf("argument validation failed: %w", err)
	}

	cmd := exec.Command(profile.Executable, args...)
	cmd.Dir = workDir

	// Fixed environment allowlist - pull only natively from the host.
	// We do not accept environment variables from the coordinator.
	cmdEnv := []string{}
	allowed := map[string]bool{
		"HOME":            true,
		"USER":            true,
		"TMPDIR":          true,
		"TEMP":            true,
		"TMP":             true,
		"PATH":            true,
		"SystemRoot":      true,
		"LD_LIBRARY_PATH": true,
	}
	for _, envStr := range os.Environ() {
		parts := strings.SplitN(envStr, "=", 2)
		if len(parts) > 0 && allowed[parts[0]] {
			cmdEnv = append(cmdEnv, envStr)
		}
	}
	cmd.Env = cmdEnv

	configureOSProcess(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	cleanup, err := startProcess(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	outputChan := make(chan []byte, 1)
	errChan := make(chan error, 1)

	go func() {
		outBytes, errOut := io.ReadAll(stdout)
		errBytes, errErr := io.ReadAll(stderr)

		if errOut != nil {
			errChan <- fmt.Errorf("stdout read error: %w", errOut)
			return
		}
		if errErr != nil {
			errChan <- fmt.Errorf("stderr read error: %w", errErr)
			return
		}

		outputChan <- append(outBytes, errBytes...)
	}()

	var output []byte
	var readErr error

	select {
	case <-ctx.Done():
		terminateProcess(cmd)
		select {
		case out := <-outputChan:
			output = out
		case err := <-errChan:
			readErr = err
		default:
		}
		return output, fmt.Errorf("execution cancelled: %w", ctx.Err())
	case err := <-errChan:
		readErr = err
		terminateProcess(cmd)
		cmd.Wait()
		return nil, fmt.Errorf("output reading failed: %w", readErr)
	case out := <-outputChan:
		output = out
	}

	if err := cmd.Wait(); err != nil {
		return output, fmt.Errorf("process exited with error: %w", err)
	}

	return output, nil
}
