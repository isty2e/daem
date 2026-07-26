package resolution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func TestResolveBatchRealGitPathFailureDoesNotBlockSibling(t *testing.T) {
	requireResolutionGit(t)
	tempDir := t.TempDir()
	repositoryPath := initResolutionGitRepository(t, tempDir)
	writeResolutionGitFile(t, repositoryPath, "skills/valid/SKILL.md", "---\nname: valid\ndescription: valid\n---\n")
	commitResolutionGit(t, repositoryPath, "add valid skill")
	resolver := newResolutionGitResolver(t, tempDir)
	requests := []acquisition.Request{
		batchRequest("skill:000000", 0, acquisition.OperationResolve, mustGitSource(t, repositoryPath, "skills/missing", "main")),
		batchRequest("skill:000001", 1, acquisition.OperationResolve, mustGitSource(t, repositoryPath, "skills/valid", "main")),
	}
	events := &repositoryBatchEventRecorder{}

	results, err := resolver.ResolveBatch(context.Background(), requests, acquisition.NewBatchOptions(2, events.record))
	if err != nil {
		t.Fatalf("ResolveBatch returned top-level error: %v", err)
	}
	if len(results) != 2 || results[0].Err() == nil || results[1].Err() != nil {
		t.Fatalf("results = %#v, want missing-path failure and valid sibling success", results)
	}
	resolution, ok := results[1].Resolution()
	if !ok || resolution.Identity().SourceID() == "" || resolution.Identity().ContentHash() == "" {
		t.Fatalf("valid sibling resolution is incomplete: %#v", results[1])
	}
	if got := events.requestIDs(acquisition.EventFetch); len(got) != 1 || got[0] != requests[0].ID() {
		t.Fatalf("fetch event request IDs = %#v, want stable representative %q", got, requests[0].ID())
	}
}

func TestResolveBatchRealGitFailedRefRetriesInFreshSession(t *testing.T) {
	requireResolutionGit(t)
	tempDir := t.TempDir()
	repositoryPath := initResolutionGitRepository(t, tempDir)
	writeResolutionGitFile(t, repositoryPath, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	writeResolutionGitFile(t, repositoryPath, "skills/beta/SKILL.md", "---\nname: beta\ndescription: beta\n---\n")
	commitResolutionGit(t, repositoryPath, "add skills")
	resolver := newResolutionGitResolver(t, tempDir)
	requests := []acquisition.Request{
		batchRequest("skill:000000", 0, acquisition.OperationResolve, mustGitSource(t, repositoryPath, "skills/alpha", "future")),
		batchRequest("skill:000001", 1, acquisition.OperationResolve, mustGitSource(t, repositoryPath, "skills/beta", "future")),
	}
	firstEvents := &repositoryBatchEventRecorder{}

	failed, err := resolver.ResolveBatch(context.Background(), requests, acquisition.NewBatchOptions(2, firstEvents.record))
	if err != nil {
		t.Fatalf("first ResolveBatch returned top-level error: %v", err)
	}
	if len(failed) != 2 || failed[0].Err() == nil || failed[1].Err() == nil {
		t.Fatalf("first results = %#v, want per-request missing-ref errors", failed)
	}
	if got := firstEvents.requestIDs(acquisition.EventFetch); len(got) != 1 || got[0] != requests[0].ID() {
		t.Fatalf("first fetch request IDs = %#v, want %q", got, requests[0].ID())
	}

	runResolutionGit(t, repositoryPath, "branch", "future")
	secondEvents := &repositoryBatchEventRecorder{}
	retried, err := resolver.ResolveBatch(context.Background(), requests, acquisition.NewBatchOptions(2, secondEvents.record))
	if err != nil {
		t.Fatalf("retry ResolveBatch returned error: %v", err)
	}
	for index, result := range retried {
		resolution, ok := result.Resolution()
		if result.Err() != nil || !ok || resolution.Identity().SourceID() == "" {
			t.Fatalf("retry result[%d] = %#v, want success", index, result)
		}
	}
	if got := secondEvents.requestIDs(acquisition.EventFetch); len(got) != 1 || got[0] != requests[0].ID() {
		t.Fatalf("retry fetch request IDs = %#v, want %q", got, requests[0].ID())
	}
}

func newResolutionGitResolver(t *testing.T, tempDir string) Resolver {
	t.Helper()
	paths, err := daempaths.Resolve(filepath.Join(tempDir, "project", "daem.toml"))
	if err != nil {
		t.Fatalf("resolve test paths returned error: %v", err)
	}
	resolver, err := NewResolver(paths)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	return resolver
}

func requireResolutionGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is required")
	}
}

func initResolutionGitRepository(t *testing.T, tempDir string) string {
	t.Helper()
	repositoryPath := filepath.Join(tempDir, "repository")
	runResolutionGit(t, "", "init", repositoryPath)
	runResolutionGit(t, repositoryPath, "checkout", "-b", "main")
	runResolutionGit(t, repositoryPath, "config", "user.email", "daem@example.invalid")
	runResolutionGit(t, repositoryPath, "config", "user.name", "Agent Env Test")
	return repositoryPath
}

func writeResolutionGitFile(t *testing.T, repositoryPath string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(repositoryPath, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func commitResolutionGit(t *testing.T, repositoryPath string, message string) string {
	t.Helper()
	runResolutionGit(t, repositoryPath, "add", ".")
	runResolutionGit(t, repositoryPath, "commit", "-m", message)
	return runResolutionGit(t, repositoryPath, "rev-parse", "HEAD")
}

func runResolutionGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v returned error: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}
