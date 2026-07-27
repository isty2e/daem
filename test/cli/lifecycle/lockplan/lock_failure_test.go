package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockDoesNotReplaceExistingLockfileOnFailure(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/bad/README.md", "not a skill\n")

	originalLockfile := []byte("version = 1\n# keep me\n")
	if err := os.WriteFile(lockfilePath, originalLockfile, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "bad"
source = { path = "skills/bad", mode = "vendor" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}

	currentLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if string(currentLockfile) != string(originalLockfile) {
		t.Fatalf("lockfile was replaced on failure:\n%s", currentLockfile)
	}
}

func TestRunLockMissingInstructionSourcePrintsNextSteps(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/missing.md"
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		`source path "` + filepath.Join(tempDir, "instructions", "missing.md") + `" does not exist`,
		"next: create the missing source file or directory, edit the manifest source path, or remove the resource declaration",
		"next: run " + testkit.ExpectedShellCommand(t, "daem", "lock", "--manifest", manifestPath, "--dry-run"),
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if strings.Contains(stderr.String(), "daem init") {
		t.Fatalf("stderr = %q, existing manifest was misclassified as missing", stderr.String())
	}
	testkit.AssertPathMissing(t, lockfilePath)
}

func TestRunLockDoesNotReplaceExistingLockfileOnInvalidManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")

	originalLockfile := []byte("version = 1\n# keep invalid manifest\n")
	if err := os.WriteFile(lockfilePath, originalLockfile, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["unknown"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}

	currentLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if string(currentLockfile) != string(originalLockfile) {
		t.Fatalf("lockfile was replaced on invalid manifest:\n%s", currentLockfile)
	}
}

func TestRunLockDoesNotReplaceExplicitLockfileWhenHookSourceIsDeclared(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "locks", "daem.lock.toml")

	originalLockfile := []byte("version = 1\n# keep explicit\n")
	if err := os.MkdirAll(filepath.Dir(lockfilePath), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(lockfilePath, originalLockfile, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
command = "python3 hooks/missing.py"
source = { path = "hooks/missing.py", mode = "vendor" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}

	if !strings.Contains(stderr.String(), `unknown manifest key "hook.source"`) {
		t.Fatalf("stderr = %q, want strict unknown hook source diagnostic", stderr.String())
	}

	currentLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if string(currentLockfile) != string(originalLockfile) {
		t.Fatalf("explicit lockfile was replaced on failure:\n%s", currentLockfile)
	}
}

func TestRunLockRejectsUnexpectedArguments(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "extra"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}

	if !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("stderr = %q, want unexpected argument diagnostic", stderr.String())
	}
}

func TestRunLockRejectsUnknownFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--unknown"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}

	if !strings.Contains(stderr.String(), "flag provided but not defined: -unknown") {
		t.Fatalf("stderr = %q, want unknown flag diagnostic", stderr.String())
	}

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunLockReportsInvalidSkillAndDoesNotWriteLockfile(t *testing.T) {
	for _, scenario := range []struct {
		name string
		args func(manifestPath string) []string
	}{
		{
			name: "write",
			args: func(manifestPath string) []string {
				return []string{"lock", "--manifest", manifestPath}
			},
		},
		{
			name: "dry-run",
			args: func(manifestPath string) []string {
				return []string{"lock", "--manifest", manifestPath, "--dry-run"}
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			testkit.WriteFile(t, tempDir, "skills/bad/README.md", "not a skill\n")

			if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "bad"
source = { path = "skills/bad", mode = "vendor" }
`), 0o600); err != nil {
				t.Fatalf("WriteFile returned error: %v", err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := testkit.RunVerboseCLI(scenario.args(manifestPath), &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1", exitCode)
			}

			if !strings.Contains(stderr.String(), "missing SKILL.md") {
				t.Fatalf("stderr = %q, want missing SKILL.md diagnostic", stderr.String())
			}

			if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
				t.Fatalf("lockfile exists or stat failed unexpectedly: %v", err)
			}
		})
	}
}

func TestRunLockRejectsCodexSkillMissingDescription(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/review/SKILL.md", "---\nname: review\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "target \"codex\"") || !strings.Contains(stderr.String(), "description is required") {
		t.Fatalf("stderr = %q, want codex missing description diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("lockfile exists or stat failed unexpectedly: %v", err)
	}
}
