package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func RequireGit(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git executable is unavailable: %v", err)
	}
}

func InitGitRepository(t *testing.T, root string) string {
	t.Helper()

	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	RunGit(t, repoPath, "init")
	RunGit(t, repoPath, "checkout", "-b", "main")
	RunGit(t, repoPath, "config", "user.email", "daem@example.invalid")
	RunGit(t, repoPath, "config", "user.name", "Agent Env Test")

	return repoPath
}

func CommitRepository(t *testing.T, repoPath string, message string) string {
	t.Helper()

	RunGit(t, repoPath, "add", ".")
	RunGit(t, repoPath, "commit", "-m", message)

	return strings.TrimSpace(RunGit(t, repoPath, "rev-parse", "HEAD"))
}

func RunGit(t *testing.T, repoPath string, args ...string) string {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = repoPath
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}

	return string(output)
}
