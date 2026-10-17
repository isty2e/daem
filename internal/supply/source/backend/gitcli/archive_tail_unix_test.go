//go:build darwin || linux

package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/iotest"
	"time"

	sourcearchive "github.com/isty2e/daem/internal/supply/source/archive"
)

func TestExtractGitArchiveDrainsFiniteTail(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, executable, "-test.run=^TestGitArchiveFiniteTailHelper$")
	command.Env = append(os.Environ(), "DAEM_GIT_ARCHIVE_FINITE_TAIL_HELPER=1")
	root := t.TempDir()

	if err := extractGitArchiveCommand(ctx, command, root); err != nil {
		t.Fatalf("finite archive producer failed: %v", err)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("finite archive required the diagnostic deadline: %v", err)
	}
	if command.ProcessState == nil || !command.ProcessState.Success() {
		t.Fatalf("archive producer was not successfully waited: %v", command.ProcessState)
	}
	payload, err := os.ReadFile(filepath.Join(root, "payload.txt"))
	if err != nil || string(payload) != "payload" {
		t.Fatalf("extracted payload = %q, error = %v", payload, err)
	}
}

func TestGitArchiveFiniteTailHelper(t *testing.T) {
	if os.Getenv("DAEM_GIT_ARCHIVE_FINITE_TAIL_HELPER") != "1" {
		return
	}
	if err := writeGitHelperArchive(os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if _, err := io.Copy(os.Stdout, bytes.NewReader(make([]byte, 2<<20))); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestGitArchiveTailPreservesDrainErrorClassification(t *testing.T) {
	var archive bytes.Buffer
	if err := writeGitHelperArchive(&archive); err != nil {
		t.Fatal(err)
	}
	drainErr := &gitOutputDrainError{cause: os.ErrClosed}
	input := io.MultiReader(bytes.NewReader(archive.Bytes()), iotest.ErrReader(drainErr))
	err := sourcearchive.ExtractTar(t.Context(), input, t.TempDir())
	if !errors.Is(err, os.ErrClosed) || !gitOutputDrainOnly(err) {
		t.Fatalf("archive tail lost native drain classification: %v", err)
	}
	result := gitProcessResult{commandErr: context.Canceled, outputIncomplete: true}
	if got := gitObservedLifecycleError(err, result); !errors.Is(got, context.Canceled) {
		t.Fatalf("archive tail hid observed caller cancellation: %v", got)
	}
}
