package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunApplyInteractiveAuthorizationRequiresEveryTerminalRole(t *testing.T) {
	for mask := range 8 {
		t.Run(fmt.Sprintf("terminal-mask-%03b", mask), func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			options := RunOptions{
				Stdin:                strings.NewReader("no\n"),
				Stdout:               &stdout,
				Stderr:               &stderr,
				StdinIsTerminal:      mask&1 != 0,
				StdoutIsTerminal:     mask&2 != 0,
				StderrIsTerminal:     mask&4 != 0,
				ReadConfirmationLine: readConfirmationLineFromReader,
			}

			exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, options)
			if mask == 7 {
				if exitCode != 1 || !strings.Contains(stderr.String(), "apply canceled") {
					t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
				}
			} else {
				if exitCode != 2 || !strings.Contains(stderr.String(), "non-interactive apply requires --yes") {
					t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
				}
				if strings.Contains(stderr.String(), "Proceed with apply?") || stdout.Len() != 0 {
					t.Fatalf("stdout = %q stderr = %q, want refusal before planning", stdout.String(), stderr.String())
				}
			}
			assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
			assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
		})
	}
}

func TestRunApplyMissingStreamRefusesWithoutPanicOrMutation(t *testing.T) {
	tests := []struct {
		name          string
		missingStdin  bool
		missingStdout bool
		missingStderr bool
	}{
		{name: "stdin", missingStdin: true},
		{name: "stdout", missingStdout: true},
		{name: "stderr", missingStderr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			options := RunOptions{
				Stdin:                strings.NewReader("yes\n"),
				Stdout:               &stdout,
				Stderr:               &stderr,
				StdinIsTerminal:      true,
				StdoutIsTerminal:     true,
				StderrIsTerminal:     true,
				ReadConfirmationLine: readConfirmationLineFromReader,
			}
			if test.missingStdin {
				options.Stdin = nil
			}
			if test.missingStdout {
				options.Stdout = nil
			}
			if test.missingStderr {
				options.Stderr = nil
			}

			if exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, options); exitCode != 2 {
				t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
			}
			assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
			assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
		})
	}
}

func TestRunApplyDisclosureWriteFailureDoesNotPromptOrMutate(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
	input := &countingReader{reader: strings.NewReader("yes\n")}
	stdoutErr := errors.New("stdout closed")
	var stderr bytes.Buffer
	options := interactiveRunOptions(input, errorWriter{err: stdoutErr}, &stderr)

	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, options)
	if exitCode != 1 || !strings.Contains(stderr.String(), "apply failed: disclose plan: stdout closed") {
		t.Fatalf("exitCode = %d stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stderr.String(), "Proceed with apply?") || input.reads != 0 {
		t.Fatalf("stderr = %q input reads = %d, want no prompt or input", stderr.String(), input.reads)
	}
	assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunApplyDisclosureShortWriteDoesNotPromptOrMutate(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
	input := &countingReader{reader: strings.NewReader("yes\n")}
	var stderr bytes.Buffer
	options := interactiveRunOptions(input, shortWriter{}, &stderr)

	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, options)
	if exitCode != 1 || !strings.Contains(stderr.String(), io.ErrShortWrite.Error()) {
		t.Fatalf("exitCode = %d stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stderr.String(), "Proceed with apply?") || input.reads != 0 {
		t.Fatalf("stderr = %q input reads = %d, want no prompt or input", stderr.String(), input.reads)
	}
	assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunApplyPartialDisclosureFailureDoesNotPromptOrMutate(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
	input := &countingReader{reader: strings.NewReader("yes\n")}
	stdoutErr := errors.New("stdout closed after partial plan")
	stdout := &failAfterSuccessfulWrites{remaining: 1, err: stdoutErr}
	var stderr bytes.Buffer
	options := interactiveRunOptions(input, stdout, &stderr)

	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, options)
	if exitCode != 1 || !strings.Contains(stderr.String(), stdoutErr.Error()) {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.buffer.String(), stderr.String())
	}
	if stdout.buffer.Len() == 0 || strings.Contains(stderr.String(), "Proceed with apply?") || input.reads != 0 {
		t.Fatalf("stdout = %q stderr = %q input reads = %d", stdout.buffer.String(), stderr.String(), input.reads)
	}
	assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunApplyPromptWriteFailureDoesNotReadOrMutate(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
	input := &countingReader{reader: strings.NewReader("yes\n")}
	var stdout bytes.Buffer
	options := interactiveRunOptions(input, &stdout, errorWriter{err: errors.New("stderr closed")})

	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, options)
	if exitCode != 1 || input.reads != 0 {
		t.Fatalf("exitCode = %d input reads = %d stdout = %q", exitCode, input.reads, stdout.String())
	}
	assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunApplyPromptTerminatorFailureDoesNotMutateAfterYes(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
	input := &countingReader{reader: strings.NewReader("yes\n")}
	var stdout bytes.Buffer
	stderr := &failAfterSuccessfulWrites{remaining: 1, err: errors.New("stderr closed after prompt")}
	options := interactiveRunOptions(input, &stdout, stderr)

	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, options)
	if exitCode != 1 || input.reads == 0 || !strings.Contains(stderr.buffer.String(), "Proceed with apply?") {
		t.Fatalf("exitCode = %d input reads = %d stderr = %q", exitCode, input.reads, stderr.buffer.String())
	}
	assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
}

type shortWriter struct{}

func (shortWriter) Write(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	return len(payload) - 1, nil
}
