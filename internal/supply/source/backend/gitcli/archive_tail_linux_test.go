package gitcli

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestExtractGitArchiveDrainsRealGitSmallPipe(t *testing.T) {
	requireGit(t)
	repository := initGitRepository(t, t.TempDir())
	payload := strings.Repeat("x", 8<<10)
	writeGitTestFile(t, repository, "payload.txt", payload)
	commit := commitAll(t, repository, "archive tail fixture")
	archive := []byte(runGitTestCommand(t, repository, "archive", "--format=tar", commit))
	input := bytes.NewReader(archive)
	reader := tar.NewReader(input)
	for {
		_, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("inspect real Git tar: %v", err)
		}
	}
	tailBytes := input.Len()
	if tailBytes == 0 {
		t.Fatal("real Git fixture leaves no raw tail")
	}

	gitPath, err := exec.LookPath(gitExecutable)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capacityFile := filepath.Join(t.TempDir(), "pipe-capacity")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestGitArchiveSmallPipeHelper$")
	command.Env = append(
		os.Environ(),
		"DAEM_GIT_ARCHIVE_SMALL_PIPE_CAPACITY="+capacityFile,
		"DAEM_GIT_ARCHIVE_SMALL_PIPE_EXECUTABLE="+gitPath,
		"DAEM_GIT_ARCHIVE_SMALL_PIPE_REPOSITORY="+repository,
		"DAEM_GIT_ARCHIVE_SMALL_PIPE_COMMIT="+commit,
	)
	root := t.TempDir()

	if err := extractGitArchiveCommand(ctx, command, root); err != nil {
		t.Fatalf("real Git archive from a small pipe: %v", err)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("real Git archive required the diagnostic deadline: %v", err)
	}
	if command.ProcessState == nil || !command.ProcessState.Success() {
		t.Fatalf("real Git producer was not successfully waited: %v", command.ProcessState)
	}
	capacityText, err := os.ReadFile(capacityFile)
	if err != nil {
		t.Fatalf("read actual pipe capacity: %v", err)
	}
	capacity, err := strconv.Atoi(string(capacityText))
	if err != nil || capacity <= 0 {
		t.Fatalf("invalid pipe capacity %q: %v", capacityText, err)
	}
	if capacity >= tailBytes {
		t.Skipf("minimum pipe capacity %d is not below the real Git tail %d", capacity, tailBytes)
	}
	content, err := os.ReadFile(filepath.Join(root, "payload.txt"))
	if err != nil || string(content) != payload {
		t.Fatalf("extracted real Git payload length=%d, error=%v", len(content), err)
	}
	t.Logf("real Git raw tail=%d bytes, pipe capacity=%d bytes", tailBytes, capacity)
}

func TestGitArchiveSmallPipeHelper(t *testing.T) {
	capacityFile := os.Getenv("DAEM_GIT_ARCHIVE_SMALL_PIPE_CAPACITY")
	if capacityFile == "" {
		return
	}
	capacity, err := unix.FcntlInt(os.Stdout.Fd(), unix.F_SETPIPE_SZ, 4096)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.WriteFile(capacityFile, []byte(strconv.Itoa(capacity)), 0o600); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	gitPath := os.Getenv("DAEM_GIT_ARCHIVE_SMALL_PIPE_EXECUTABLE")
	args := []string{
		gitPath, "-C", os.Getenv("DAEM_GIT_ARCHIVE_SMALL_PIPE_REPOSITORY"),
		"archive", "--format=tar", os.Getenv("DAEM_GIT_ARCHIVE_SMALL_PIPE_COMMIT"),
	}
	if err := syscall.Exec(gitPath, args, os.Environ()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
