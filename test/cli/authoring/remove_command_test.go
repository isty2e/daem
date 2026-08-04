package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/lockfile"
)

func TestRunRemoveSkillYesUpdatesManifestAndLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	hostSkillPath := filepath.Join(tempDir, ".agents", "skills", "oracle", "SKILL.md")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "https://github.com/owner/repo.git", path = "skills/oracle", ref = "main" }
targets = ["codex"]
`)
	testkit.WriteFile(t, tempDir, "daem.lock.toml", "version = 5\n\n[locked]\n")
	testkit.WriteFile(t, filepath.Dir(statefilePath), filepath.Base(statefilePath), "state stays\n")
	testkit.WriteFile(t, filepath.Dir(hostSkillPath), filepath.Base(hostSkillPath), "host stays\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "skill", "oracle", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "removed: skill/oracle") || !strings.Contains(stdout.String(), "change: remove skill resource") {
		t.Fatalf("stdout = %q, want remove summary", stdout.String())
	}
	for _, want := range []string{
		"lockfile: wrote " + lockfilePath,
		"next: run daem apply --manifest " + manifestPath + " --dry-run",
		"note: remove updates the manifest and lockfile only; host files are deleted only when apply reconciles managed state",
	} {
		testkit.AssertOutputLine(t, stdout.String(), want)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Skills()) != 0 {
		t.Fatalf("skills = %#v, want none", config.Skills())
	}
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 0 || len(testkit.LockedInstructions(t, locked)) != 0 || len(testkit.LockedHooks(t, locked)) != 0 {
		t.Fatalf("locked = %#v, want no resources", locked.Locked)
	}
	testkit.AssertFileContent(t, statefilePath, "state stays\n")
	testkit.AssertFileContent(t, hostSkillPath, "host stays\n")
}

func TestRunRemoveSkillDryRunReportsManifestAuthoringNextSteps(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	original := `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "https://github.com/owner/repo.git", path = "skills/oracle", ref = "main" }
targets = ["codex"]
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "skill", "oracle", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"remove: skill/oracle",
		"change: remove skill resource",
		"lockfile: would write " + lockfilePath,
		"next: rerun this authoring command without --dry-run",
		"note: remove updates the manifest and lockfile only; host files are deleted only when apply reconciles managed state",
	} {
		testkit.AssertOutputLine(t, stdout.String(), want)
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, lockfilePath)
}

func TestRunRemoveSkillDryRunDiffShowsResultingManifestDelta(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "https://github.com/owner/repo.git", path = "skills/oracle", ref = "main" }
targets = ["codex"]
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "skill", "oracle", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"manifest diff:",
		"--- " + manifestPath,
		"+++ " + manifestPath,
		"-[[skill]]",
		`-name = "oracle"`,
		`-targets = ["codex"]`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunRemoveSkillYesLeavesFilesUnchangedWhenProspectiveLockFails(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	original := `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/missing-review", mode = "vendor" }
targets = ["codex"]
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
	testkit.WriteFile(t, tempDir, "daem.lock.toml", "lock stays\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "skill", "oracle", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "remove failed: lock prospective manifest") {
		t.Fatalf("stderr = %q, want prospective lock failure", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertFileContent(t, lockfilePath, "lock stays\n")
}

func TestRunRemoveSkillGuidesPartialSkillGroupTargetRemoval(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `
version = 1
targets = ["codex", "claude-code"]

[[skill_group]]
names = ["alpha", "beta"]
source = { git = "https://github.com/owner/repo.git", path = "skills", ref = "main" }
targets = ["codex", "claude-code"]
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "skill", "alpha", "--manifest", manifestPath, "--target", "codex", "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		`skill_group member "alpha" shares targets with other members`,
		`next: edit the manifest so "alpha" is in its own [[skill_group]] block with targets = ["claude-code"]`,
		"next: keep the same source, scope, and install_mode on the original and split skill_group blocks",
		"next: then rerun daem remove skill alpha --target <target> --dry-run",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunRemoveSkillRejectsAmbiguousInstallName(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `
version = 1
targets = ["codex", "claude-code"]

[[skill]]
id = "codex-review"
name = "review"
source = { git = "https://github.com/owner/codex.git", path = "skills/review", ref = "main" }
targets = ["codex"]

[[skill]]
id = "claude-review"
name = "review"
source = { git = "https://github.com/owner/claude.git", path = "skills/review", ref = "main" }
targets = ["claude-code"]
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "skill", "review", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ambiguous") {
		t.Fatalf("stderr = %q, want ambiguous diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunRemoveSkillReportsMissingResource(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "skill", "missing", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), `skill resource "missing" not found`) {
		t.Fatalf("stderr = %q, want missing resource diagnostic", stderr.String())
	}
}

func TestRunRemoveSkillRejectsSelectorBackedChildWithGuidance(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
exclude = ["glob:draft-*"]
targets = ["codex"]
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "skill", "alpha", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		`selector-backed skill_group children are not edited by remove skill`,
		`edit include/exclude selectors manually`,
		`run daem lock`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
}
