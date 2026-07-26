package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunListReportsManifestAuthoringIdentifiers(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "AGENTS.md"
targets = ["codex"]

[[skill]]
id = "codex-review"
name = "review"
source = { git = "https://github.com/owner/repo.git", path = "skills/review", ref = "main" }
targets = ["codex"]
scope = "global"

[[skill_group]]
names = ["alpha", "beta"]
source = { path = "skills", mode = "vendor" }
targets = ["claude-code"]

[[hook]]
name = "prime-session"
event = "SessionStart"
command = "bd prime"
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"list", "resources", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"manifest: " + manifestPath,
		"resources: 5",
		`resource kind=instructions key="project" install="-" source="local:AGENTS.md?mode=vendor" targets="codex" scope="project" group="-"`,
		`resource kind=skill key="alpha" install="alpha" source="local:skills/alpha?mode=vendor" targets="claude-code" scope="project" group="skill_group[0]"`,
		`resource kind=skill key="beta" install="beta" source="local:skills/beta?mode=vendor" targets="claude-code" scope="project" group="skill_group[0]"`,
		`resource kind=skill key="codex-review" install="review" source="git:locator=https%3A%2F%2Fgithub.com%2Fowner%2Frepo.git&path=skills%2Freview&ref=name%3Amain" targets="codex" scope="global" group="-"`,
		`resource kind=hook key="prime-session" install="-" source="command-hook" targets="codex" scope="project" group="-"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunListDoesNotRequireLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"list", "resources", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "resources: 0") {
		t.Fatalf("stdout = %q, want empty manifest resource count", stdout.String())
	}
}

func TestRunListMissingManifestPrintsInitHint(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"list", "resources", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"list failed: read manifest:",
		"next: run " + testkit.ExpectedShellCommand(t, "daem", "init", "--manifest", manifestPath, "--dry-run"),
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}
