package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestImportModesRefuseActiveRecoveryBeforeLiveScan(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "dry-run", args: []string{"--dry-run"}},
		{name: "write"},
		{name: "dry-run JSON", args: []string{"--dry-run", "--json"}},
		{name: "write JSON", args: []string{"--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			testkit.WithWorkingDirectory(t, root)
			testkit.WriteFile(t, root, "AGENTS.md", "must not be imported\n")
			manifestPath := filepath.Join(root, "daem.toml")
			createActiveRecoveryMarker(t, root)
			args := []string{"import", "--target", "codex", "--manifest", manifestPath}
			args = append(args, test.args...)
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, stdout = %q, stderr = %q, want 1", exitCode, stdout.String(), stderr.String())
			}
			assertActiveRecoveryRefusal(t, stdout.String(), stderr.String())
			if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
				t.Fatalf("manifest exists or stat failed unexpectedly: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "daem.d")); !os.IsNotExist(err) {
				t.Fatalf("import source directory exists or stat failed unexpectedly: %v", err)
			}
		})
	}
}

func TestDoctorPresentationsRefuseActiveRecoveryBeforeHostChecks(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "human"},
		{name: "json", args: []string{"--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			testkit.WithWorkingDirectory(t, root)
			manifestPath := filepath.Join(root, "daem.toml")
			createActiveRecoveryMarker(t, root)
			args := []string{"doctor", "--manifest", manifestPath, "--target", "codex"}
			args = append(args, test.args...)
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, stdout = %q, stderr = %q, want 1", exitCode, stdout.String(), stderr.String())
			}
			assertActiveRecoveryRefusal(t, stdout.String(), stderr.String())
		})
	}
}

func TestLockAndOutdatedRemainAvailableDuringActiveRecovery(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "instructions.md", "desired instructions\n")
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions.md"

[instructions.project.target.codex]
render_to = "AGENTS.md"
`)
	createActiveRecoveryMarker(t, root)

	for _, invocation := range [][]string{
		{"lock", "--manifest", manifestPath, "--dry-run"},
		{"lock", "--manifest", manifestPath},
		{"outdated", "--manifest", manifestPath},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := testkit.RunVerboseCLI(invocation, &stdout, &stderr)
		if exitCode != 0 {
			t.Fatalf("invocation %v exitCode = %d, stdout = %q, stderr = %q", invocation, exitCode, stdout.String(), stderr.String())
		}
		if strings.Contains(stderr.String(), "daem recover") {
			t.Fatalf("lock-only invocation %v was blocked by active recovery: %q", invocation, stderr.String())
		}
	}
}

func createActiveRecoveryMarker(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".daem", "recovery", "active-operation"), 0o700); err != nil {
		t.Fatalf("create active recovery marker: %v", err)
	}
}

func assertActiveRecoveryRefusal(t *testing.T, stdout string, stderr string) {
	t.Helper()
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"interrupted apply operation found", "daem recover --dry-run"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want %q", stderr, want)
		}
	}
}
