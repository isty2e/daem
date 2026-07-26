package gitcli

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func requireGit(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath(gitExecutable); err != nil {
		t.Skipf("git executable is unavailable: %v", err)
	}
}

func mustGitSource(t *testing.T, locator string, repositoryPath string, ref string) source.Source {
	t.Helper()

	sourceSpec, err := source.NewGitSource(locator, repositoryPath, ref)
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}
	return sourceSpec
}

func initGitRepository(t *testing.T, root string) string {
	t.Helper()

	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	runGitTestCommand(t, repoPath, "init")
	runGitTestCommand(t, repoPath, "checkout", "-b", "main")
	runGitTestCommand(t, repoPath, "config", "user.email", "daem@example.invalid")
	runGitTestCommand(t, repoPath, "config", "user.name", "Agent Env Test")

	return repoPath
}

func commitAll(t *testing.T, repoPath string, message string) string {
	t.Helper()

	runGitTestCommand(t, repoPath, "add", ".")
	runGitTestCommand(t, repoPath, "commit", "-m", message)

	return strings.TrimSpace(runGitTestCommand(t, repoPath, "rev-parse", "HEAD"))
}

func writeGitTestFile(t *testing.T, root string, relativePath string, content string) string {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return path
}

func runGitTestCommand(t *testing.T, repoPath string, args ...string) string {
	t.Helper()

	command := exec.Command(gitExecutable, args...)
	command.Dir = repoPath
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}

	return string(output)
}

func assertNoTemporaryArtifacts(t *testing.T, artifactParent string) {
	t.Helper()

	entries, err := os.ReadDir(artifactParent)
	if os.IsNotExist(err) {
		return
	}

	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}

	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("temporary artifact was left behind: %s", entry.Name())
		}
	}
}

func cacheEntryExists(root string) bool {
	info, err := os.Lstat(root)
	return err == nil && info.IsDir()
}

func hasGitEventKind(events []acquisition.Event, kind acquisition.EventKind) bool {
	for _, event := range events {
		if event.Kind() == kind {
			return true
		}
	}

	return false
}

func countGitEventKind(events []acquisition.Event, kind acquisition.EventKind) int {
	count := 0
	for _, event := range events {
		if event.Kind() == kind {
			count++
		}
	}

	return count
}

func mustGitAcquisitionRequest(
	t *testing.T,
	id acquisition.RequestID,
	ordinal int,
	operation acquisition.Operation,
	sourceSpec source.Source,
) acquisition.Request {
	t.Helper()
	request, err := acquisition.NewRequest(id, ordinal, operation, sourceSpec)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	return request
}

func mustGitOperationOptions(
	t *testing.T,
	request acquisition.Request,
	events acquisition.EventSink,
) acquisition.OperationOptions {
	t.Helper()
	options, err := acquisition.NewOperationOptions(request, events)
	if err != nil {
		t.Fatalf("NewOperationOptions returned error: %v", err)
	}
	return options
}

func mustReadGitResolutionFile(
	t *testing.T,
	resolution acquisition.Resolution,
	relativePath string,
) []byte {
	t.Helper()
	content, err := resolution.View().ReadFile(context.Background(), relativePath, 1<<20)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", relativePath, err)
	}
	return content.Bytes()
}

func assertGitResolutionIdentity(
	t *testing.T,
	resolution acquisition.Resolution,
	wantSourceID artifact.SourceID,
	wantResolvedRef artifact.ResolvedRef,
	wantKind artifact.ArtifactKind,
) artifact.ExactIdentity {
	t.Helper()
	identity := resolution.Identity()
	if identity.SourceID() != wantSourceID {
		t.Fatalf("SourceID = %q, want %q", identity.SourceID(), wantSourceID)
	}
	if identity.ResolvedRef() != wantResolvedRef {
		t.Fatalf("ResolvedRef = %q, want %q", identity.ResolvedRef(), wantResolvedRef)
	}
	if identity.Kind() != wantKind {
		t.Fatalf("Kind = %q, want %q", identity.Kind(), wantKind)
	}
	if identity.ContentHash() == "" {
		t.Fatal("ContentHash is empty")
	}
	return identity
}

func gitTestArchive(t *testing.T, header tar.Header, content string) []byte {
	t.Helper()

	return gitTestArchiveWithHeaders(t, []gitTestTarEntry{{header: header, content: content}})
}

type gitTestTarEntry struct {
	header  tar.Header
	content string
}

func gitTestArchiveWithHeaders(t *testing.T, entries []gitTestTarEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)

	for _, entry := range entries {
		header := entry.header
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatalf("WriteHeader returned error: %v", err)
		}

		if entry.content != "" {
			if _, err := writer.Write([]byte(entry.content)); err != nil {
				t.Fatalf("Write returned error: %v", err)
			}
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	return buffer.Bytes()
}
