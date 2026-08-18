package gitcli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/isty2e/daem/internal/subprocess"
)

type gitProcess struct {
	stdout           *os.File
	stderr           *os.File
	group            *subprocess.ProcessGroup
	waitDone         chan gitProcessWait
	stderrDone       chan error
	diagnostic       gitDiagnosticBuffer
	diagnosticPolicy subprocess.CapturePolicy
}

type gitProcessWait struct {
	waitErr        error
	termination    subprocess.ProcessTermination
	terminationErr error
}

type gitProcessResult struct {
	commandErr     error
	termination    subprocess.ProcessTermination
	terminationErr error
	stderrReadErr  error
}

func startGitProcess(command *exec.Cmd) (*gitProcess, error) {
	if command == nil {
		return nil, fmt.Errorf("start git process: command is required")
	}
	environment := subprocess.ChildEnvironmentFrom(command.Environ())
	diagnosticPolicy := subprocess.NewCapturePolicy(
		environment.SecretValues(),
		maxGitDiagnosticRunes,
	)

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("open git stdout pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		return nil, fmt.Errorf("open git stderr pipe: %w", err)
	}
	closePipes := func() {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
	}

	command.Stdout = stdoutWriter
	command.Stderr = stderrWriter
	group, err := subprocess.BindProcessGroup(command)
	if err != nil {
		closePipes()
		return nil, fmt.Errorf("supervise git process group: %w", err)
	}
	if err := command.Start(); err != nil {
		closePipes()
		return nil, err
	}
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	process := &gitProcess{
		stdout:           stdoutReader,
		stderr:           stderrReader,
		group:            group,
		waitDone:         make(chan gitProcessWait, 1),
		stderrDone:       make(chan error, 1),
		diagnosticPolicy: diagnosticPolicy,
	}
	go func() {
		_, readErr := io.Copy(&process.diagnostic, process.stderr)
		process.stderrDone <- readErr
	}()
	go func() {
		waitErr := command.Wait()
		termination, terminationErr := group.ReapAfterLeaderExit()
		process.waitDone <- gitProcessWait{
			waitErr:        waitErr,
			termination:    termination,
			terminationErr: terminationErr,
		}
	}()
	return process, nil
}

func (process *gitProcess) Stdout() io.Reader {
	return process.stdout
}

func (process *gitProcess) Terminate() (subprocess.ProcessTermination, error) {
	return process.group.Terminate()
}

func (process *gitProcess) Wait() gitProcessResult {
	wait := <-process.waitDone
	_ = process.stdout.Close()
	stderrReadErr := <-process.stderrDone
	_ = process.stderr.Close()
	commandErr := wait.waitErr
	if commandErr != nil {
		commandErr = gitCommandErrorWithCapture(
			process.diagnosticPolicy,
			commandErr,
			process.diagnostic.String(),
			process.diagnostic.Truncated(),
		)
	}
	return gitProcessResult{
		commandErr:     commandErr,
		termination:    wait.termination,
		terminationErr: wait.terminationErr,
		stderrReadErr:  stderrReadErr,
	}
}

func joinGitProcessGroupTerminateErr(base error, role string, result gitProcessResult) error {
	if result.termination.UnsignalableOccupancy() {
		cause := result.terminationErr
		if cause == nil {
			cause = errors.New("process group occupancy is unsignalable")
		}
		return errors.Join(base, fmt.Errorf("%s process group occupancy is unsignalable: %w", role, cause))
	}
	if result.terminationErr != nil {
		return errors.Join(base, fmt.Errorf("terminate %s process group: %w", role, result.terminationErr))
	}
	return base
}

func joinGitProcessGroupResidual(base error, role string, result gitProcessResult) error {
	base = joinGitProcessGroupTerminateErr(base, role, result)
	if result.termination.UnsignalableOccupancy() {
		return base
	}
	if result.termination.ProcessesFound() {
		return errors.Join(base, fmt.Errorf("%s exited while process-group members remained; terminated residual process group", role))
	}
	return base
}
