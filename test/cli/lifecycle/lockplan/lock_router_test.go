package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockDryRun(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "instructions\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	if !strings.Contains(stdout.String(), lockfilePath) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
		t.Fatalf("lock --dry-run wrote lockfile or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("lock --dry-run wrote state/cache directory or stat failed unexpectedly: %v", err)
	}
}

func TestRunLockDryRunSummarizesGeneratedSupplyAndProjectionSubjects(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "instructions\n")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
command = "python3 hooks/protect.py"
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	for _, want := range []string{
		"would write lockfile: " + lockfilePath,
		"lockfile entries: subjects=5",
		"locked.subject:",
		"  - projection/codex.project.hooks/hook:protect-env",
		"  - projection/instructions.project.agents/instructions:project",
		"  - resource/instructions/project",
		"  - projection/skill.project.agents/skill:oracle",
		"  - resource/skill/oracle",
		"next: run " + testkit.ExpectedShellCommand(t, "daem", "lock", "--manifest", manifestPath),
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestPublicExampleManifestsLockDryRun(t *testing.T) {
	for _, name := range []string{"daem.toml", "representative-project.toml", "skill-placement.toml"} {
		t.Run(name, func(t *testing.T) {
			manifestPath := filepath.Join(testkit.RepositoryRoot(t), "examples", name)

			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
			if exitCode != 0 {
				t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			if !strings.Contains(stdout.String(), "would write lockfile") {
				t.Fatalf("stdout = %q, want dry-run lockfile summary", stdout.String())
			}
		})
	}
}

func TestPublicSkillPlacementExampleListsSelectedRoots(t *testing.T) {
	manifestPath := filepath.Join(testkit.RepositoryRoot(t), "examples", "skill-placement.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"list", "paths", "--manifest", manifestPath},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "write: .agents/skills [selected, default]") {
		t.Fatalf("list paths output = %q, want selected Codex default", output)
	}
	if count := strings.Count(output, "write: .agents/skills [selected]"); count != 2 {
		t.Fatalf("list paths selected compatible root count = %d, want 2\n%s", count, output)
	}
}

func TestRunLockDryRunReportsInvalidManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["unknown-agent"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}

	if !strings.Contains(stderr.String(), "unknown target") {
		t.Fatalf("stderr = %q, want unknown target", stderr.String())
	}
}
