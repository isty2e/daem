package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyDryRunReportsSelectedLockOnlyResources(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeLockOnlyManifest(t, manifestPath)
	writeLockOnlyLockfile(t, lockfilePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "lock-only: skills=0 hooks=1 (unsupported or not reconciled by apply/status)") {
		t.Fatalf("stdout = %q, want hook-only lock-only summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "dry-run: 1 actions") {
		t.Fatalf("stdout = %q, want supported skill plan", stdout.String())
	}
}

func TestRunApplyYesReportsSelectedLockOnlyResources(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeLockOnlyManifest(t, manifestPath)
	writeLockOnlyLockfile(t, lockfilePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "opencode", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "lock-only:") {
		t.Fatalf("stdout = %q, want no lock-only summary for supported opencode skill", stdout.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want one applied skill action", stdout.String())
	}
}

func TestRunStatusReportsSelectedLockOnlyResources(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeLockOnlyManifest(t, manifestPath)
	writeLockOnlyLockfile(t, lockfilePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--target", "pi"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "lock-only: skills=0 hooks=1 (unsupported or not reconciled by apply/status)") {
		t.Fatalf("stdout = %q, want selected lock-only summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "  - hook/protect-env targets=pi") {
		t.Fatalf("stdout = %q, want selected lock-only hook details", stdout.String())
	}
	if !strings.Contains(stdout.String(), "status: 0 actions") {
		t.Fatalf("stdout = %q, want empty status plan", stdout.String())
	}
}

func TestRunApplyDryRunReportsLockOnlyResourcesAlongsideInstructionActions(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "opencode", "pi"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]

[[hook]]
name = "protect-env"
event = "pre_apply"
command = "python3 hooks/protect_env.py"
targets = ["pi"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Use for oracle review.\n---\n")
	skillHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
		Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash,
		Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
	}, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"lock-only: skills=0 hooks=1 (unsupported or not reconciled by apply/status)",
		"dry-run: 2 actions",
		`create resource="skill/oracle" target=opencode`,
		`create resource="instructions/project" target=codex`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunApplyDryRunOmitsLockOnlySummaryForInstructionOnlyManifest(t *testing.T) {
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
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "lock-only:") {
		t.Fatalf("stdout = %q, want no lock-only summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "dry-run: 1 actions") {
		t.Fatalf("stdout = %q, want instruction plan", stdout.String())
	}
}
