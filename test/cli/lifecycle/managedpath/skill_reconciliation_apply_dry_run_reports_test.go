package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyDryRunReportsMissingSkillLockAsPlanError(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"dry-run: 1 actions",
		`error resource="skill/oracle" target=codex scope=project destination=".agents/skills/oracle" mode=copy reason=missing_lock`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunStatusRejectsOpenCodeSkillFrontmatterMismatch(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: other\ndescription: Use for oracle review.\n---\n")
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
		Kind:        testkit.ExactSupplySkill,
		Name:        "oracle",
		SourceID:    "local:skills/oracle?mode=vendor",
		ContentHash: skillHash,
		Targets:     []target.Target{target.TargetOpenCode},
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--target", "opencode"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `name "other" must match skill name "oracle"`) {
		t.Fatalf("stderr = %q, want opencode frontmatter mismatch diagnostic", stderr.String())
	}
}

func TestRunStatusReportsSupportedSkillCreateAndUnmanagedConflict(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	testkit.WriteFile(t, tempDir, ".agents/skills/manual/SKILL.md", "---\nname: manual\ndescription: manual\n---\n")
	oracleHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))
	manualHash := testkit.HashDirectory(t, filepath.Join(tempDir, ".agents/skills/manual"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[[skill]]
name = "manual"
source = { path = ".agents/skills/manual", mode = "vendor" }
targets = ["codex"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "manual", SourceID: "local:.agents/skills/manual?mode=vendor", ContentHash: manualHash}, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: oracleHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"status: 2 actions",
		`create resource="skill/oracle" target=codex scope=project destination=".agents/skills/oracle" mode=copy reason=missing_output`,
		`error resource="skill/manual" target=codex scope=project destination=".agents/skills/manual" mode=copy reason=unmanaged_output_exists`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunApplyDryRunRejectsUnsafeSkillDestinationName(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "../oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "safe") {
		t.Fatalf("stderr = %q, want safe path diagnostic", stderr.String())
	}
}

func TestRunStatusRejectsFileAtManagedSkillDirectoryDestination(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	testkit.WriteFile(t, tempDir, ".agents/skills/oracle", "not a directory\n")
	skillHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.SkillPathState(t, "oracle", []string{"codex"}, "project", ".agents/skills/oracle", "sha256:old-dir"),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `observe destination ".agents/skills/oracle": expected directory`) {
		t.Fatalf("stderr = %q, want expected directory diagnostic", stderr.String())
	}
}

func TestRunApplyYesManagesMatchingUnmanagedSkillDirectory(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	if err := testkit.CopyDirectory(filepath.Join(tempDir, "skills/oracle"), filepath.Join(tempDir, ".agents/skills/oracle")); err != nil {
		t.Fatalf("testkit.CopyDirectory returned error: %v", err)
	}
	skillHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--manage-existing", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want record action", stdout.String())
	}

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "project", ".agents/skills/oracle", skillHash)
}
