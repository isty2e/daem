package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockRejectsInstructionTargetWithNonAdmittedRenderTo(t *testing.T) {
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
			testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
			sourcePath := filepath.Join(tempDir, "instructions/AGENTS.md")

			if err := os.WriteFile(manifestPath, fmt.Appendf(nil, `
version = 1
targets = ["antigravity-cli"]

	[instructions.global]
	source = %q
	targets = ["antigravity-cli"]

	[instructions.global.target.antigravity-cli]
	render_to = "AGENTS.md"
	`, sourcePath), 0o600); err != nil {
				t.Fatalf("WriteFile returned error: %v", err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := testkit.RunVerboseCLI(scenario.args(manifestPath), &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1", exitCode)
			}
			for _, want := range []string{
				`instructions "global" target "antigravity-cli": render_to "AGENTS.md"`,
				`not an admitted instruction placement destination`,
			} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
				t.Fatalf("lock wrote lockfile after unsupported target or stat failed unexpectedly: %v", err)
			}
		})
	}
}

func TestRunApplyDryRunRejectsInstructionTargetWithNonAdmittedRenderTo(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	sourcePath := filepath.Join(tempDir, "instructions/AGENTS.md")
	instructionHash := testkit.HashPath(t, sourcePath)

	if err := os.WriteFile(manifestPath, fmt.Appendf(nil, `
version = 1
targets = ["antigravity-cli"]

	[instructions.global]
	source = %q
	targets = ["antigravity-cli"]

	[instructions.global.target.antigravity-cli]
	render_to = "AGENTS.md"
	`, sourcePath), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "global", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	for _, want := range []string{
		`instructions "global" target "antigravity-cli": render_to "AGENTS.md"`,
		`not an admitted instruction placement destination`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunStatusRejectsInstructionTargetWithNonAdmittedRenderTo(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	sourcePath := filepath.Join(tempDir, "instructions/AGENTS.md")

	if err := os.WriteFile(manifestPath, fmt.Appendf(nil, `
version = 1
targets = ["antigravity-cli"]

	[instructions.global]
	source = %q
	targets = ["antigravity-cli"]

	[instructions.global.target.antigravity-cli]
	render_to = "AGENTS.md"
	`, sourcePath), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	for _, want := range []string{
		`instructions "global" target "antigravity-cli": render_to "AGENTS.md"`,
		`not an admitted instruction placement destination`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyAllowsSupportedTargetSelectionWhenInstructionAlsoHasUnsupportedTarget(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "opencode"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex", "opencode"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash, Targets: []target.Target{target.TargetCodex, target.TargetOpenCode}}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "codex", "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `create resource="instructions/project" target=codex`) {
		t.Fatalf("stdout = %q, want codex create action", stdout.String())
	}
	if strings.Contains(stdout.String(), "opencode") {
		t.Fatalf("stdout = %q, want unsupported non-selected target omitted", stdout.String())
	}
}
