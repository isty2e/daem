package gitcli

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/isty2e/daem/internal/subprocess"
)

type gitProcess struct {
	stdout     *os.File
	stderr     *os.File
	group      *subprocess.ProcessGroup
	waitDone   chan gitProcessWait
	stderrDone chan error
	diagnostic gitDiagnosticBuffer
}

type gitProcessWait struct {
	waitErr        error
	termination    subprocess.ProcessTermination
	terminationErr error
}

type gitProcessResult struct {
	waitErr         error
	termination     subprocess.ProcessTermination
	terminationErr  error
	stderr          string
	stderrTruncated bool
	stderrReadErr   error
}

func startGitProcess(command *exec.Cmd) (*gitProcess, error) {
	if command == nil {
		return nil, fmt.Errorf("start git process: command is required")
	}

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
		return nil, fmt.Errorf("supervise git process tree: %w", err)
	}
	if err := command.Start(); err != nil {
		closePipes()
		return nil, err
	}
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	process := &gitProcess{
		stdout:     stdoutReader,
		stderr:     stderrReader,
		group:      group,
		waitDone:   make(chan gitProcessWait, 1),
		stderrDone: make(chan error, 1),
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
	return gitProcessResult{
		waitErr:         wait.waitErr,
		termination:     wait.termination,
		terminationErr:  wait.terminationErr,
		stderr:          process.diagnostic.String(),
		stderrTruncated: process.diagnostic.Truncated(),
		stderrReadErr:   stderrReadErr,
	}
}
