//go:build darwin || linux

package subprocess

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const descriptorHelperEnvironment = "DAEM_INTERNAL_DESCRIPTOR_WORKDIR_V1"

type testWorkingDirectoryBinding struct {
	directory    *os.File
	validate     func() error
	nilDirectory bool
	closeCount   int
}

func TestExecuteInWorkingDirectoryPreservesArgvStdinAndStripsHelperMarker(t *testing.T) {
	t.Setenv("GO_WANT_COMMAND_EXEC_HELPER_PROCESS", "1")
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	newBinding := func(t *testing.T) *testWorkingDirectoryBinding {
		t.Helper()
		directory, openErr := os.Open(root)
		if openErr != nil {
			t.Fatalf("open working directory: %v", openErr)
		}
		return &testWorkingDirectoryBinding{directory: directory}
	}
	executor := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout, OutputLimit: 4096})
	wantArgs := []string{"space arg", "semi;colon", "$(echo nope)", "", "star*"}
	argvBinding := newBinding(t)

	argvResult := executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: append(
			[]string{"-test.run=TestCommandExecHelperProcess", "--", "argv"},
			wantArgs...,
		),
	}, func() (WorkingDirectoryBinding, error) { return argvBinding, nil })
	if !argvResult.Succeeded() {
		t.Fatalf("argv result = %#v", argvResult)
	}
	var gotArgs []string
	if err := json.Unmarshal([]byte(argvResult.Stdout()), &gotArgs); err != nil {
		t.Fatalf("decode argv stdout %q: %v", argvResult.Stdout(), err)
	}
	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("argv = %#v, want %#v", gotArgs, wantArgs)
	}

	stdinBinding := newBinding(t)
	stdinResult := executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args:    []string{"-test.run=TestCommandExecHelperProcess", "--", "stdin"},
		Stdin:   "descriptor-bound stdin",
	}, func() (WorkingDirectoryBinding, error) { return stdinBinding, nil })
	if !stdinResult.Succeeded() || stdinResult.Stdout() != "descriptor-bound stdin" {
		t.Fatalf("stdin result = %#v", stdinResult)
	}

	envBinding := newBinding(t)
	envResult := executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args:    []string{"-test.run=TestCommandExecHelperProcess", "--", "env-present", descriptorHelperEnvironment},
	}, func() (WorkingDirectoryBinding, error) { return envBinding, nil })
	if !envResult.Succeeded() {
		t.Fatalf("env result = %#v", envResult)
	}
	if envResult.Stdout() != "absent" {
		t.Fatalf("target helper marker state = %q, want absent", envResult.Stdout())
	}
}

func TestExecuteInWorkingDirectoryClassifiesDescriptorHelperTargetExecFailure(t *testing.T) {
	root := t.TempDir()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatalf("open working directory: %v", err)
	}
	binding := &testWorkingDirectoryBinding{directory: directory}
	command := filepath.Join(t.TempDir(), "invalid-interpreter")
	if err := os.WriteFile(command, []byte("#!/definitely/missing/daem-test-interpreter\n"), 0o700); err != nil {
		t.Fatalf("write invalid interpreter fixture: %v", err)
	}

	result := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout}).ExecuteInWorkingDirectory(
		context.Background(),
		CommandAttemptRequest{Command: command},
		func() (WorkingDirectoryBinding, error) { return binding, nil },
	)

	if result.Reason() != CommandReasonRunnerError || !result.Started() {
		t.Fatalf("result = %#v, want started descriptor-helper runner error", result)
	}
	if !strings.Contains(result.Stderr(), "target exec failed") {
		t.Fatalf("stderr = %q, want target exec failure", result.Stderr())
	}
}

func TestExecuteInWorkingDirectoryDoesNotTrustSuccessfulTargetHelperLikeOutput(t *testing.T) {
	root := t.TempDir()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatalf("open working directory: %v", err)
	}
	binding := &testWorkingDirectoryBinding{directory: directory}
	command := filepath.Join(t.TempDir(), "marker-output")
	script := "#!/bin/sh\nprintf 'daem internal target exec failed: spoofed\\n' >&2\nexit 0\n"
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatalf("write marker output fixture: %v", err)
	}

	result := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout}).ExecuteInWorkingDirectory(
		context.Background(),
		CommandAttemptRequest{Command: command},
		func() (WorkingDirectoryBinding, error) { return binding, nil },
	)

	if !result.Succeeded() {
		t.Fatalf("result = %#v, successful target output must not impersonate helper failure", result)
	}
}

func TestExecuteInWorkingDirectoryTimesOutTargetAfterDescriptorHelperExec(t *testing.T) {
	root := t.TempDir()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatalf("open working directory: %v", err)
	}
	binding := &testWorkingDirectoryBinding{directory: directory}
	executor := NewCommandExecutor(CommandOptions{Timeout: 20 * time.Millisecond, OutputLimit: 1024})

	startedAt := time.Now()
	result := executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: "/bin/sleep",
		Args:    []string{"5"},
	}, func() (WorkingDirectoryBinding, error) { return binding, nil })

	if result.Reason() != CommandReasonTimeout || !result.Started() || !result.TimedOut() {
		t.Fatalf("timeout result = %#v", result)
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("descriptor-bound timeout took %s, want prompt child termination", elapsed)
	}
}

func TestExecuteInWorkingDirectoryKeepsConcurrentRootsIndependent(t *testing.T) {
	parent := t.TempDir()
	type fixture struct {
		root string
		want string
	}
	fixtures := []fixture{
		{root: filepath.Join(parent, "first"), want: "first-root"},
		{root: filepath.Join(parent, "second"), want: "second-root"},
	}
	for _, item := range fixtures {
		if err := os.Mkdir(item.root, 0o700); err != nil {
			t.Fatalf("create %s: %v", item.root, err)
		}
		if err := os.WriteFile(filepath.Join(item.root, "sentinel"), []byte(item.want), 0o600); err != nil {
			t.Fatalf("write %s sentinel: %v", item.root, err)
		}
	}
	executor := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout, OutputLimit: 1024})
	start := make(chan struct{})
	results := make([]CommandAttemptResult, len(fixtures))
	errs := make([]error, len(fixtures))
	var wait sync.WaitGroup
	for index, item := range fixtures {
		wait.Go(func() {
			directory, err := os.Open(item.root)
			if err != nil {
				errs[index] = err
				return
			}
			binding := &testWorkingDirectoryBinding{directory: directory}
			<-start
			results[index] = executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{
				Command: "/bin/cat",
				Args:    []string{"sentinel"},
			}, func() (WorkingDirectoryBinding, error) { return binding, nil })
		})
	}
	close(start)
	wait.Wait()

	for index, item := range fixtures {
		if errs[index] != nil {
			t.Fatalf("open %s: %v", item.root, errs[index])
		}
		if !results[index].Succeeded() || results[index].Stdout() != item.want {
			t.Fatalf("result %d = %#v, want %q", index, results[index], item.want)
		}
	}
}

func (binding *testWorkingDirectoryBinding) Validate() error {
	if binding.validate != nil {
		return binding.validate()
	}
	return nil
}

func (binding *testWorkingDirectoryBinding) OpenDirectory() (*os.File, error) {
	if binding.nilDirectory {
		return nil, nil
	}
	if binding.directory == nil {
		return nil, errors.New("test working directory is unavailable")
	}
	directory := binding.directory
	binding.directory = nil
	return directory, nil
}

func (binding *testWorkingDirectoryBinding) Close() error {
	binding.closeCount++
	if binding.directory != nil {
		err := binding.directory.Close()
		binding.directory = nil
		return err
	}
	return nil
}

func TestExecuteInWorkingDirectoryRejectsNilDescriptorWithoutPanic(t *testing.T) {
	binding := &testWorkingDirectoryBinding{nilDirectory: true}
	called := false
	executor := NewCommandExecutor(CommandOptions{
		Runner: func(context.Context, CommandRequest) CommandResult {
			called = true
			return CommandResult{}
		},
	})

	result := executor.ExecuteInWorkingDirectory(
		context.Background(),
		CommandAttemptRequest{Command: "must-not-run"},
		func() (WorkingDirectoryBinding, error) {
			return binding, nil
		},
	)

	if result.Reason() != CommandReasonWorkDirAuthority ||
		!strings.Contains(result.ErrorDetail(), "descriptor is required") {
		t.Fatalf("result = %#v, want nil descriptor authority failure", result)
	}
	if called {
		t.Fatal("runner was called with a nil working-directory descriptor")
	}
}

func TestExecuteInWorkingDirectoryLaunchesFromDescriptorAfterLexicalReplacement(t *testing.T) {
	parent := t.TempDir()
	selected := filepath.Join(parent, "project")
	moved := filepath.Join(parent, "captured-project")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatalf("create selected directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(selected, "sentinel"), []byte("captured"), 0o600); err != nil {
		t.Fatalf("write captured sentinel: %v", err)
	}
	directory, err := os.Open(selected)
	if err != nil {
		t.Fatalf("open selected directory: %v", err)
	}
	if err := os.Rename(selected, moved); err != nil {
		t.Fatalf("move selected directory: %v", err)
	}
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatalf("create lexical replacement: %v", err)
	}
	if err := os.WriteFile(filepath.Join(selected, "sentinel"), []byte("replacement"), 0o600); err != nil {
		t.Fatalf("write replacement sentinel: %v", err)
	}
	binding := &testWorkingDirectoryBinding{directory: directory}
	executor := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout, OutputLimit: 1024})

	result := executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: "/bin/cat",
		Args:    []string{"sentinel"},
		WorkDir: selected,
	}, func() (WorkingDirectoryBinding, error) {
		return binding, nil
	})

	if !result.Succeeded() {
		t.Fatalf("descriptor-backed result = %#v", result)
	}
	if result.Stdout() != "captured" {
		t.Fatalf("child sentinel = %q, want captured root content instead of replacement", result.Stdout())
	}
	if binding.closeCount != 1 {
		t.Fatalf("binding close count = %d, want 1", binding.closeCount)
	}
}

func TestExecuteInWorkingDirectoryRejectsPrelaunchAndPostlaunchAuthorityFailure(t *testing.T) {
	root := t.TempDir()
	openDirectory := func(t *testing.T) *os.File {
		t.Helper()
		directory, err := os.Open(root)
		if err != nil {
			t.Fatalf("open working directory: %v", err)
		}
		return directory
	}

	t.Run("prelaunch", func(t *testing.T) {
		called := false
		binding := &testWorkingDirectoryBinding{
			directory: openDirectory(t),
			validate:  func() error { return errors.New("root replaced before launch") },
		}
		executor := NewCommandExecutor(CommandOptions{Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			called = true
			return CommandResult{Started: true, HasExitCode: true}
		}})
		result := executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "bound-test"}, func() (WorkingDirectoryBinding, error) {
			return binding, nil
		})
		if called || result.Started() || result.Reason() != CommandReasonWorkDirAuthority {
			t.Fatalf("prelaunch result = %#v called=%t, want unstarted workdir authority failure", result, called)
		}
		if binding.closeCount != 1 {
			t.Fatalf("prelaunch binding close count = %d, want 1", binding.closeCount)
		}
	})

	t.Run("postlaunch", func(t *testing.T) {
		validations := 0
		binding := &testWorkingDirectoryBinding{
			directory: openDirectory(t),
			validate: func() error {
				validations++
				if validations >= 2 {
					return errors.New("root replaced after launch")
				}
				return nil
			},
		}
		executor := NewCommandExecutor(CommandOptions{Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			return CommandResult{Started: true, HasExitCode: true}
		}})
		result := executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "bound-test"}, func() (WorkingDirectoryBinding, error) {
			return binding, nil
		})
		if !result.Started() || result.Reason() != CommandReasonWorkDirAuthority {
			t.Fatalf("postlaunch result = %#v, want started workdir authority failure", result)
		}
		if binding.closeCount != 1 {
			t.Fatalf("postlaunch binding close count = %d, want 1", binding.closeCount)
		}
	})
}
