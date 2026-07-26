package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runWithoutTerminal(args []string, stdout io.Writer, stderr io.Writer) int {
	return RunWithOptions(args, RunOptions{Stdout: stdout, Stderr: stderr})
}

func TestRunWithOptionsCanceledContextStopsBeforeMutation(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions([]string{"init", "--manifest", manifestPath}, RunOptions{
		Context: ctx,
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), context.Canceled.Error()) {
		t.Fatalf("stderr = %q, want cancellation", stderr.String())
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("manifest stat error = %v, want absent", err)
	}
}

func TestRunWithOptionsNilContextKeepsLibraryCompatibility(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions([]string{"help"}, RunOptions{Stdout: &stdout, Stderr: &stderr})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("help output is empty")
	}
}

func TestRunWithOptionsParseErrorDoesNotEmitProgress(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions([]string{"lock", "--unknown"}, RunOptions{
		Stdout:           &stdout,
		Stderr:           &stderr,
		StderrIsTerminal: true,
	})

	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("stderr = %q, want flag diagnostic", stderr.String())
	}
	if strings.Contains(stderr.String(), "progress:") {
		t.Fatalf("stderr = %q, did not want progress", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunWithOptionsCommandHelpDoesNotEmitProgress(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions([]string{"outdated", "--help"}, RunOptions{
		Stdout:           &stdout,
		Stderr:           &stderr,
		StderrIsTerminal: true,
	})

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if strings.Contains(stdout.String(), "progress:") || strings.Contains(stderr.String(), "progress:") {
		t.Fatalf("stdout = %q, stderr = %q, did not want progress", stdout.String(), stderr.String())
	}
}

func TestRunWithOptionsApplyPromptCancellationDoesNotEmitProgress(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions(
		[]string{"apply", "--manifest", manifestPath},
		interactiveRunOptions(strings.NewReader("no\n"), &stdout, &stderr),
	)

	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "apply canceled") {
		t.Fatalf("stderr = %q, want cancellation diagnostic", stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") || strings.Contains(stderr.String(), "progress:") {
		t.Fatalf("stdout = %q, stderr = %q, did not want progress before accepted confirmation", stdout.String(), stderr.String())
	}
}

func TestRunWithOptionsApplyRendererWriteFailureDoesNotFailApply(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
	stderr := &cliFailAfterFirstWrite{}
	var stdout bytes.Buffer

	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath, "--yes"}, RunOptions{
		Stdout:           &stdout,
		Stderr:           stderr,
		StderrIsTerminal: true,
	})

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.buffer.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want final apply summary", stdout.String())
	}
	if got := strings.Count(stderr.buffer.String(), "Applying 0/1"); got != 1 {
		t.Fatalf("stderr progress updates = %d, want one successful write before suppression; stderr = %q", got, stderr.buffer.String())
	}
	assertApplyConfirmationFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "shared instructions\n")
}

type cliFailAfterFirstWrite struct {
	buffer        bytes.Buffer
	writeAttempts int
}

func (writer *cliFailAfterFirstWrite) Write(payload []byte) (int, error) {
	writer.writeAttempts++
	if writer.writeAttempts > 1 {
		return 0, errors.New("stderr closed")
	}

	return writer.buffer.Write(payload)
}
