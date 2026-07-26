//go:build darwin || linux

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/isty2e/daem/internal/cli"
)

const (
	confirmationPTYHelperMode = "DAEM_CONFIRMATION_PTY_HELPER"
	subprocessTestTimeout     = 30 * time.Second
)

func TestMainConfirmationPTYRejectsRedirectedOrPipedRoles(t *testing.T) {
	tests := []struct {
		name       string
		stdin      io.Reader
		stdoutTTY  bool
		stderrTTY  bool
		diagnostic func(*confirmationPTYProcess) string
	}{
		{
			name:      "stdout redirected",
			stdoutTTY: false,
			stderrTTY: true,
			diagnostic: func(process *confirmationPTYProcess) string {
				return process.terminal.String()
			},
		},
		{
			name:      "stderr redirected",
			stdoutTTY: true,
			stderrTTY: false,
			diagnostic: func(process *confirmationPTYProcess) string {
				return process.stderr.String()
			},
		},
		{
			name:      "stdin piped",
			stdin:     strings.NewReader("yes\n"),
			stdoutTTY: true,
			stderrTTY: true,
			diagnostic: func(process *confirmationPTYProcess) string {
				return process.terminal.String()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath := writePTYApplyFixture(t)
			process := startConfirmationPTYProcess(t, manifestPath, test.stdin, test.stdoutTTY, test.stderrTTY)
			if exitCode := process.Wait(t); exitCode != 2 {
				t.Fatalf("exit code = %d, terminal = %q stdout = %q stderr = %q", exitCode, process.terminal.String(), process.stdout.String(), process.stderr.String())
			}
			diagnostic := test.diagnostic(process)
			if !strings.Contains(diagnostic, "non-interactive apply requires --yes") || strings.Contains(diagnostic, "Proceed with apply?") {
				t.Fatalf("diagnostic = %q", diagnostic)
			}
			assertPTYApplyNotMutated(t, manifestPath)
		})
	}
}

func TestMainConfirmationPTYDeclineEOFAcceptance(t *testing.T) {
	tests := []struct {
		name       string
		answer     []byte
		wantExit   int
		wantOutput string
		wantApply  bool
	}{
		{name: "decline", answer: []byte("no\n"), wantExit: 1, wantOutput: "apply canceled"},
		{name: "eof", answer: []byte{4}, wantExit: 1, wantOutput: "apply canceled"},
		{name: "affirmative then eof", answer: []byte{'y', 'e', 's', 4}, wantExit: 1, wantOutput: "apply canceled"},
		{name: "success", answer: []byte("yes\n"), wantExit: 0, wantOutput: "applied: 1 actions", wantApply: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath := writePTYApplyFixture(t)
			process := startConfirmationPTYProcess(t, manifestPath, nil, true, true)
			process.WaitForOutput(t, "Proceed with apply? [y/N]:")
			if _, err := process.master.Write(test.answer); err != nil {
				t.Fatalf("write PTY answer: %v", err)
			}
			if exitCode := process.Wait(t); exitCode != test.wantExit {
				t.Fatalf("exit code = %d, want %d; terminal = %q", exitCode, test.wantExit, process.terminal.String())
			}
			if !strings.Contains(process.terminal.String(), test.wantOutput) {
				t.Fatalf("terminal = %q, want %q", process.terminal.String(), test.wantOutput)
			}
			if test.wantApply {
				assertPTYApplyContent(t, manifestPath, "shared instructions\n")
			} else {
				assertPTYApplyNotMutated(t, manifestPath)
			}
		})
	}
}

func TestMainConfirmationPTYCtrlCInterruptsBlockedRead(t *testing.T) {
	manifestPath := writePTYApplyFixture(t)
	process := startConfirmationPTYProcess(t, manifestPath, nil, true, true)
	process.WaitForOutput(t, "Proceed with apply? [y/N]:")

	if err := process.command.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send SIGINT: %v", err)
	}
	if exitCode := process.Wait(t); exitCode != 130 {
		t.Fatalf("exit code = %d, want 130; terminal = %q", exitCode, process.terminal.String())
	}
	if !strings.Contains(process.terminal.String(), "apply canceled: context canceled") {
		t.Fatalf("terminal = %q, want context cancellation", process.terminal.String())
	}
	assertPTYApplyNotMutated(t, manifestPath)
}

func TestConfirmationPTYHelperProcess(t *testing.T) {
	if os.Getenv(confirmationPTYHelperMode) == "" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		t.Fatal("confirmation PTY helper arguments are missing")
	}
	os.Args = append([]string{"daem"}, os.Args[separator+1:]...)
	main()
}

type confirmationPTYProcess struct {
	command  *exec.Cmd
	master   *os.File
	terminal synchronizedBuffer
	stdout   bytes.Buffer
	stderr   bytes.Buffer
	copyDone chan struct{}
}

func startConfirmationPTYProcess(t *testing.T, manifestPath string, stdin io.Reader, stdoutTTY bool, stderrTTY bool) *confirmationPTYProcess {
	return startConfirmationPTYProcessWithEnv(t, manifestPath, stdin, stdoutTTY, stderrTTY)
}

func startConfirmationPTYProcessWithEnv(
	t *testing.T,
	manifestPath string,
	stdin io.Reader,
	stdoutTTY bool,
	stderrTTY bool,
	extraEnv ...string,
) *confirmationPTYProcess {
	return startConfirmationPTYCommandProcessWithEnv(
		t,
		[]string{"apply", "--manifest", manifestPath},
		stdin,
		stdoutTTY,
		stderrTTY,
		extraEnv...,
	)
}

func startConfirmationPTYCommandProcessWithEnv(
	t *testing.T,
	arguments []string,
	stdin io.Reader,
	stdoutTTY bool,
	stderrTTY bool,
	extraEnv ...string,
) *confirmationPTYProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commandArguments := append([]string{"-test.run=^TestConfirmationPTYHelperProcess$", "--"}, arguments...)
	command := exec.Command(executable, commandArguments...)
	command.Env = append(os.Environ(), confirmationPTYHelperMode+"=1")
	command.Env = append(command.Env, extraEnv...)
	process := &confirmationPTYProcess{command: command, copyDone: make(chan struct{})}
	if stdin != nil {
		command.Stdin = stdin
	}
	if !stdoutTTY {
		command.Stdout = &process.stdout
	}
	if !stderrTTY {
		command.Stderr = &process.stderr
	}

	attributes := &syscall.SysProcAttr{}
	if stdin == nil {
		attributes.Setsid = true
		attributes.Setctty = true
	}
	master, err := pty.StartWithAttrs(command, nil, attributes)
	if err != nil {
		t.Fatalf("start PTY helper: %v", err)
	}
	process.master = master
	go func() {
		_, _ = io.Copy(&process.terminal, master)
		close(process.copyDone)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = master.Close()
	})
	return process
}

func (process *confirmationPTYProcess) WaitForOutput(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(subprocessTestTimeout)
	for time.Now().Before(deadline) {
		if strings.Contains(process.terminal.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; terminal = %q", want, process.terminal.String())
}

func (process *confirmationPTYProcess) Wait(t *testing.T) int {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- process.command.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(subprocessTestTimeout):
		_ = process.command.Process.Kill()
		waitErr = <-done
		t.Fatalf("timed out waiting for PTY helper; terminal = %q, wait error = %v", process.terminal.String(), waitErr)
	}
	_ = process.master.Close()
	select {
	case <-process.copyDone:
	case <-time.After(time.Second):
		t.Fatalf("timed out draining PTY output")
	}
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("wait PTY helper: %v", waitErr)
	}
	return exitErr.ExitCode()
}

type synchronizedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(payload []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.Write(payload)
}

func (buffer *synchronizedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.String()
}

func writePTYApplyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.MkdirAll(filepath.Join(root, "instructions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "instructions", "AGENTS.md"), []byte("shared instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	lockPTYManifest(t, manifestPath)
	return manifestPath
}

func lockPTYManifest(t *testing.T, manifestPath string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := cli.RunWithOptions(
		[]string{"lock", "--manifest", manifestPath},
		cli.RunOptions{Stdout: &stdout, Stderr: &stderr},
	); exitCode != 0 {
		t.Fatalf("lock exit code = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}

func assertPTYApplyNotMutated(t *testing.T, manifestPath string) {
	t.Helper()
	root := filepath.Dir(manifestPath)
	for _, path := range []string{filepath.Join(root, "AGENTS.md"), filepath.Join(root, ".daem")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("path %q exists or stat failed unexpectedly: %v", path, err)
		}
	}
}

func assertPTYApplyContent(t *testing.T, manifestPath string, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(filepath.Dir(manifestPath), "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("applied content = %q, want %q", content, want)
	}
}
