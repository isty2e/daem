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

func TestRunApplyStatusLockOnlyTargetSelectionEdgeCases(t *testing.T) {
	scenarios := []struct {
		name       string
		setup      func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string
		wantExit   int
		wantStdout []string
		wantStderr []string
		reject     []string
		check      func(t *testing.T, stdout string, stderr string)
	}{
		{
			name: "opencode skill-only target is reconciled for apply dry-run",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Use for oracle review.\n---\n")
				skillHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
					Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash,
					Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
				}))
				return []string{"apply", "--manifest", manifestPath, "--target", "opencode", "--dry-run"}
			},
			wantExit: 0,
			wantStdout: []string{
				"dry-run: 1 actions",
				`create resource="skill/oracle" target=opencode`,
			},
			reject: []string{
				"lock-only:",
			},
		},
		{
			name: "pi hook-only target is valid for status",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["pi"]

[[hook]]
name = "notify"
event = "pre_apply"
command = "python3 hooks/notify.py"
targets = ["pi"]
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t))
				return []string{"status", "--manifest", manifestPath, "--target", "pi"}
			},
			wantExit: 0,
			wantStdout: []string{
				"lock-only: skills=0 hooks=1 (unsupported or not reconciled by apply/status)",
				"  - hook/notify targets=pi",
				"status: 0 actions",
			},
		},
		{
			name: "opencode skill-only target is reconciled for mutating apply",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Use for oracle review.\n---\n")
				skillHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
					Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash,
					Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
				}))
				return []string{"apply", "--manifest", manifestPath, "--target", "opencode", "--yes"}
			},
			wantExit: 0,
			wantStdout: []string{
				"applied: 1 actions",
			},
			reject: []string{
				"lock-only:",
			},
		},
		{
			name: "instruction target invalid render_to is not hidden by lock-only resources",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: portable-oracle\ndescription: Use for oracle review.\n---\n")
				skillHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
				sourcePath := filepath.Join(tempDir, "instructions/AGENTS.md")
				testkit.WriteFile(t, tempDir, "daem.toml", fmt.Sprintf(`
version = 1
targets = ["antigravity-cli"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["antigravity-cli"]

	[instructions.global]
	source = %q
	targets = ["antigravity-cli"]

	[instructions.global.target.antigravity-cli]
	render_to = "AGENTS.md"
	`, sourcePath))
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
					Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash,
					Targets: []target.Target{target.TargetAntigravityCLI}, Scope: target.ScopeProject,
				}, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "global", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: testkit.FixtureHash("sha256:antigravity")}))
				return []string{"apply", "--manifest", manifestPath, "--target", "antigravity-cli", "--dry-run"}
			},
			wantExit: 1,
			wantStderr: []string{
				`instructions "global" target "antigravity-cli": render_to "AGENTS.md"`,
				`not an admitted instruction placement destination`,
			},
			reject: []string{
				"lock-only:",
				"dry-run:",
			},
		},
		{
			name: "repeated target flags do not duplicate selected outputs",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
				instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
					Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: testkit.FixtureHash("sha256:oracle"),
					Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
				}, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))
				return []string{"apply", "--manifest", manifestPath, "--target", "codex", "--target", "codex", "--dry-run"}
			},
			wantExit: 0,
			wantStdout: []string{
				"dry-run: 1 actions",
			},
			check: func(t *testing.T, stdout string, stderr string) {
				t.Helper()
				if count := strings.Count(stdout, `create resource="instructions/project" target=codex`); count != 1 {
					t.Fatalf("create action count = %d, stdout = %q", count, stdout)
				}
			},
		},
		{
			name: "unselected lock-only resources do not leak into apply summary",
			setup: func(t *testing.T, tempDir string, manifestPath string, lockfilePath string) []string {
				testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
				instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
				testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex", "opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
				testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
					Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: testkit.FixtureHash("sha256:oracle"),
					Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
				}, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))
				return []string{"apply", "--manifest", manifestPath, "--target", "codex", "--dry-run"}
			},
			wantExit: 0,
			wantStdout: []string{
				"dry-run: 1 actions",
				`create resource="instructions/project" target=codex`,
			},
			reject: []string{
				"lock-only:",
				"target=opencode",
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			args := scenario.setup(t, tempDir, manifestPath, lockfilePath)

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
				scenario.check(t, stdoutText, stderrText)
			}
		})
	}
}
