package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyRejectsOldLockfileBeforeSourceInspectionOrMutation(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/never-read.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
	testkit.WriteFile(t, tempDir, "daem.lock.toml", `
version = 1
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported lockfile version 1") {
		t.Fatalf("stderr = %q, want unsupported version diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("invalid lockfile apply wrote output or stat failed: %v", err)
	}
}

func TestRunStatusRejectsOldLockfileBeforeSourceInspection(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/never-read.md"
`)
	testkit.WriteFile(t, tempDir, "daem.lock.toml", "version = 1\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsupported lockfile version 1") {
		t.Fatalf("stderr = %q, want unsupported version diagnostic", stderr.String())
	}
}

func TestRunStatusRejectsMalformedLockfileBeforePlanning(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
	testkit.WriteFile(t, tempDir, "daem.lock.toml", `
version = 3

[locked]

[[locked.subject]]
entity_id = "instructions:project"
subject_id = "resource/instructions/project"
ownership = "manifest"
on_absent = "apply"

[locked.subject.exact_supply]
kind = "file"
content_hash = "`+instructionHash+`"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `exact artifact source id is required`) {
		t.Fatalf("stderr = %q, want missing source_id diagnostic", stderr.String())
	}
}
