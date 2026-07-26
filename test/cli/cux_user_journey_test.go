package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestCUXFirstProjectJourneyMatchesGettingStarted(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)

	runCUXJourney(t, []string{"init", "--dry-run"}, "init:", "next:")
	testkit.AssertPathMissing(t, filepath.Join(root, "daem.toml"))

	runCUXJourney(t, []string{"init"}, "created: manifest")
	testkit.WriteFile(t, root, "instructions/project.md", "Project instructions.\n")

	runCUXJourney(t, []string{
		"add", "instruction", "project", "./instructions/project.md",
		"--target", "codex", "--dry-run",
	}, "add: instructions/project", "lockfile: would write")
	testkit.AssertPathMissing(t, filepath.Join(root, "daem.lock.toml"))

	runCUXJourney(t, []string{
		"add", "instruction", "project", "./instructions/project.md",
		"--target", "codex",
	}, "added: instructions/project", "lockfile: wrote")

	runCUXJourney(t, []string{"status"}, "add managed output")
	runCUXJourney(t, []string{"apply", "--dry-run"}, "add managed output")
	runCUXJourney(t, []string{"apply", "--yes"}, "applied: 1 actions")

	content, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read applied AGENTS.md: %v", err)
	}
	if string(content) != "Project instructions.\n" {
		t.Fatalf("AGENTS.md = %q", content)
	}

	runCUXJourney(t, []string{"status", "--check"}, "up to date")
}

func runCUXJourney(t *testing.T, args []string, expected ...string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := testkit.RunCLI(args, &stdout, &stderr); exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("args=%q exitCode=%d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
	}
	for _, value := range expected {
		if !strings.Contains(stdout.String(), value) {
			t.Fatalf("args=%q stdout=%q, want %q", args, stdout.String(), value)
		}
	}
}
