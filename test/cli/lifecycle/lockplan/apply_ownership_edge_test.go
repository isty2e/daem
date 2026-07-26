package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyOwnershipEdgeCases(t *testing.T) {
	scenarios := []struct {
		name       string
		setup      func(t *testing.T, tempDir string, manifestPath string, lockfilePath string, statefilePath string) []string
		wantExit   int
		wantStdout []string
		wantStderr []string
		reject     []string
		check      func(t *testing.T, tempDir string)
	}{
		{
			name: "dry-run rejects directory where managed removed file should be observed",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string, statefilePath string) []string {
				writeApplyOwnershipActiveFixture(t, tempDir, manifestPath, lockfilePath)
				if err := os.MkdirAll(filepath.Join(tempDir, "CLAUDE.md"), 0o700); err != nil {
					t.Fatalf("MkdirAll returned error: %v", err)
				}
				testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
					t,
					testkit.InstructionPathState(t, "removed", []string{"claude-code"}, "project", "CLAUDE.md", "sha256:removed"),
				))
				return []string{"apply", "--manifest", manifestPath, "--dry-run"}
			},
			wantExit: 1,
			wantStderr: []string{
				`observe destination "CLAUDE.md": expected regular file`,
			},
			reject: []string{
				"delete ",
				"dry-run:",
			},
		},
		{
			name: "dry-run rejects symlink where managed removed file should be observed",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string, statefilePath string) []string {
				writeApplyOwnershipActiveFixture(t, tempDir, manifestPath, lockfilePath)
				if err := os.Symlink("elsewhere.md", filepath.Join(tempDir, "CLAUDE.md")); err != nil {
					t.Fatalf("Symlink returned error: %v", err)
				}
				testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
					t,
					testkit.InstructionPathState(t, "removed", []string{"claude-code"}, "project", "CLAUDE.md", "sha256:removed"),
				))
				return []string{"apply", "--manifest", manifestPath, "--dry-run"}
			},
			wantExit: 1,
			wantStderr: []string{
				`observe destination "CLAUDE.md": symlinks are not supported yet`,
			},
			reject: []string{
				"delete ",
				"dry-run:",
			},
		},
		{
			name: "dry-run reports missing managed removed output as blocked evidence",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string, statefilePath string) []string {
				writeApplyOwnershipActiveFixture(t, tempDir, manifestPath, lockfilePath)
				testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
					t,
					testkit.InstructionPathState(t, "removed", []string{"claude-code"}, "project", "CLAUDE.md", "sha256:removed"),
				))
				return []string{"apply", "--manifest", manifestPath, "--dry-run"}
			},
			wantExit: 1,
			wantStdout: []string{
				"dry-run: 2 actions",
				`error resource="instructions/removed" target=claude-code scope=project destination="CLAUDE.md" mode= reason=missing_output detail="managed output is missing" safety=missing_evidence`,
			},
			reject: []string{
				`delete resource="instructions/removed"`,
			},
		},
		{
			name: "manage-existing can record one output while reporting selected deletion",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string, statefilePath string) []string {
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "desired instructions\n")
				testkit.WriteFile(t, tempDir, "AGENTS.md", "desired instructions\n")
				testkit.WriteFile(t, tempDir, "CLAUDE.md", "old managed\n")
				desiredHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
				oldHash := testkit.HashPath(t, filepath.Join(tempDir, "CLAUDE.md"))
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: desiredHash}))
				testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
					t,
					testkit.InstructionPathState(t, "old", []string{"claude-code"}, "project", "CLAUDE.md", oldHash),
				))
				return []string{"apply", "--manifest", manifestPath, "--manage-existing", "--dry-run"}
			},
			wantExit: 0,
			wantStdout: []string{
				"dry-run: 2 actions",
				`record resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=managed_existing`,
				`delete resource="instructions/old" target=claude-code scope=project destination="CLAUDE.md" mode= reason=removed_from_manifest safety=deletable`,
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			statefilePath := filepath.Join(tempDir, ".daem", "state.json")
			args := scenario.setup(t, tempDir, manifestPath, lockfilePath, statefilePath)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != scenario.wantExit {
				t.Fatalf("exitCode = %d, want %d\nstdout = %q\nstderr = %q", exitCode, scenario.wantExit, stdout.String(), stderr.String())
			}

			stdoutText := stdout.String()
			stderrText := stderr.String()
			for _, want := range scenario.wantStdout {
				if !strings.Contains(stdoutText, want) {
					t.Fatalf("stdout = %q, want %q", stdoutText, want)
				}
			}
			for _, want := range scenario.wantStderr {
				if !strings.Contains(stderrText, want) {
					t.Fatalf("stderr = %q, want %q", stderrText, want)
				}
			}
			if len(scenario.wantStderr) == 0 && stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderrText)
			}
			for _, reject := range scenario.reject {
				if strings.Contains(stdoutText, reject) || strings.Contains(stderrText, reject) {
					t.Fatalf("stdout = %q stderr = %q, did not want %q", stdoutText, stderrText, reject)
				}
			}
			if scenario.check != nil {
				scenario.check(t, tempDir)
			}
		})
	}
}

func writeApplyOwnershipActiveFixture(t *testing.T, tempDir string, manifestPath string, lockfilePath string) string {
	t.Helper()

	testkit.WriteFile(t, tempDir, "instructions/active.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "active managed\n")
	activeHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/active.md"))
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.active]
source = "instructions/active.md"

[instructions.active.target.codex]
render_to = "AGENTS.md"
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "active", SourceID: "local:instructions/active.md?mode=vendor", ContentHash: activeHash}))
	return activeHash
}
