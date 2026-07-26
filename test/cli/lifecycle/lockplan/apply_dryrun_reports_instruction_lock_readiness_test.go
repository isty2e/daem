package cli_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyDryRunReportsInstructionLockReadinessBeforeExecutableActions(t *testing.T) {
	scenarios := []struct {
		name             string
		setup            func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string
		want             []string
		reject           []string
		wantLockNextStep bool
		wantExit         int
	}{
		{
			name: "stale lock blocks manage-existing record",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "new instructions\n")
				testkit.WriteFile(t, tempDir, "AGENTS.md", "old instructions\n")
				oldHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: oldHash}))
				return []string{"--manage-existing"}
			},
			want: []string{
				"dry-run: 1 actions",
				`error resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=stale_lock`,
			},
			reject: []string{
				"reason=managed_existing",
				"record ",
			},
			wantLockNextStep: true,
			wantExit:         1,
		},
		{
			name: "stale lock blocks missing-output create",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "old instructions\n")
				oldHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "new instructions\n")
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: oldHash}))
				return nil
			},
			want: []string{
				"dry-run: 1 actions",
				`error resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=stale_lock`,
			},
			reject: []string{
				"reason=missing_output",
				"create ",
			},
			wantLockNextStep: true,
			wantExit:         1,
		},
		{
			name: "missing lock is a dry-run plan error",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t))
				return nil
			},
			want: []string{
				"dry-run: 1 actions",
				`error resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=missing_lock`,
			},
			reject: []string{
				"apply failed: plan instructions",
				"reason=missing_output",
			},
			wantLockNextStep: true,
			wantExit:         1,
		},
		{
			name: "target selection filters stale lock plan errors",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "old instructions\n")
				oldHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "new instructions\n")
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex", "claude-code"]

				[instructions.project.target.claude-code]
				render_to = "CLAUDE.md"
				`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: oldHash, Targets: []target.Target{target.TargetCodex, target.TargetClaudeCode}}))
				return []string{"--target", "claude-code"}
			},
			want: []string{
				"dry-run: 1 actions",
				`error resource="instructions/project" target=claude-code scope=project destination="CLAUDE.md" mode=copy reason=stale_lock`,
			},
			reject: []string{
				"target=codex",
				"reason=missing_output",
			},
			wantLockNextStep: true,
			wantExit:         1,
		},
		{
			name: "crlf drift is stale rather than normalized",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "line\r\n")
				oldHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "line\n")
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: oldHash}))
				return nil
			},
			want: []string{
				"dry-run: 1 actions",
				`error resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=stale_lock`,
			},
			reject: []string{
				"reason=missing_output",
				"create ",
			},
			wantLockNextStep: true,
			wantExit:         1,
		},
		{
			name: "stale lock preserves admitted global render_to destination in error",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "old instructions\n")
				sourcePath := filepath.Join(tempDir, "instructions/AGENTS.md")
				oldHash := testkit.HashPath(t, sourcePath)
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "new instructions\n")
				testkit.WriteFile(t, tempDir, "daem.toml", fmt.Sprintf(`
version = 1
targets = ["codex"]

[instructions.project]
source = %q
scope = "global"

[instructions.project.target.codex]
render_to = "AGENTS.md"
`, sourcePath))
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:" + filepath.ToSlash(sourcePath) + "?mode=vendor", ContentHash: oldHash, Scope: target.ScopeGlobal}))
				return nil
			},
			want: []string{
				"dry-run: 1 actions",
				`error resource="instructions/project" target=codex scope=global destination="~/.codex/AGENTS.md" mode=copy reason=stale_lock`,
			},
			reject: []string{
				"destination=\"AGENTS.md\"",
				"reason=missing_output",
			},
			wantLockNextStep: true,
			wantExit:         1,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			extraArgs := scenario.setup(t, tempDir, manifestPath, lockfilePath)

			args := []string{"apply", "--manifest", manifestPath, "--dry-run"}
			args = append(args, extraArgs...)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != scenario.wantExit {
				t.Fatalf("exitCode = %d, want %d\nstdout = %q\nstderr = %q", exitCode, scenario.wantExit, stdout.String(), stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}

			output := stdout.String()
			for _, want := range scenario.want {
				if !strings.Contains(output, want) {
					t.Fatalf("stdout = %q, want %q", output, want)
				}
			}
			for _, reject := range scenario.reject {
				if strings.Contains(output, reject) {
					t.Fatalf("stdout = %q, did not want %q", output, reject)
				}
			}
			if scenario.wantLockNextStep && !strings.Contains(output, "next: run daem lock --manifest") {
				t.Fatalf("stdout = %q, want lock-readiness next step", output)
			}
		})
	}
}
