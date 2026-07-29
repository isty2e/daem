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
version = 3
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(
		stderr.String(),
		"unsupported lockfile version 3; run daem lock to regenerate schema version 4",
	) {
		t.Fatalf("stderr = %q, want actionable v3 diagnostic", stderr.String())
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
	testkit.WriteFile(t, tempDir, "daem.lock.toml", "version = 3\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(
		stderr.String(),
		"unsupported lockfile version 3; run daem lock to regenerate schema version 4",
	) {
		t.Fatalf("stderr = %q, want actionable v3 diagnostic", stderr.String())
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
version = 4

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

func TestStatusAndApplyRejectContextuallyForgedExtensionOrderIdentity(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[extension]]
id = "first"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "@acme/first@1.0.0" }

[[extension]]
id = "second"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "@acme/second@1.0.0" }
`)

	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI(
		[]string{"lock", "--manifest", manifestPath},
		&lockStdout,
		&lockStderr,
	); exitCode != 0 {
		t.Fatalf(
			"lock exitCode = %d, want 0; stdout = %q stderr = %q",
			exitCode,
			lockStdout.String(),
			lockStderr.String(),
		)
	}
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	content, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(
		string(content),
		`host_load_identity = "@acme/first"`,
		`host_load_identity = "@acme/forged"`,
		1,
	)
	if forged == string(content) {
		t.Fatal("generated lockfile did not contain expected host identity")
	}
	if err := os.WriteFile(lockfilePath, []byte(forged), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "status",
			args: []string{"status", "--manifest", manifestPath},
		},
		{
			name: "apply dry-run",
			args: []string{"apply", "--manifest", manifestPath, "--dry-run"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(test.args, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf(
					"exitCode = %d, want 1; stdout = %q stderr = %q",
					exitCode,
					stdout.String(),
					stderr.String(),
				)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(
				stderr.String(),
				`host-load identity "@acme/forged" does not match derived identity "@acme/first"`,
			) {
				t.Fatalf(
					"stderr = %q, want contextual host identity diagnostic",
					stderr.String(),
				)
			}
		})
	}
}
