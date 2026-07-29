package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunLockDryRunReportsDeltaAgainstExistingLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, hashes := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	for _, want := range []string{
		"lockfile changes: added=2 changed=1 removed=2 unchanged=3",
		"lockfile.subject.added:",
		`  - resource/skill/review entity="skill:review"`,
		`  - projection/skill.project.agents/skill:review entity="skill:review"`,
		`content_hash="` + hashes.Review + `"`,
		"lockfile.subject.changed:",
		`  - resource/skill/oracle changed=exact_supply,derivation`,
		"lockfile.subject.removed:",
		`  - resource/skill/legacy entity="skill:legacy"`,
		`  - projection/skill.project.agents/skill:legacy entity="skill:legacy"`,
		"lockfile.subject.unchanged:",
		"  - resource/instructions/project",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunLockDryRunReportsSelectorExpansionDeltaAgainstExistingLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/changed/SKILL.md", "---\nname: changed\ndescription: old changed\n---\n")
	testkit.WriteFile(t, tempDir, "skills/removed/SKILL.md", "---\nname: removed\ndescription: removed\n---\n")
	testkit.WriteFile(t, tempDir, "skills/unchanged/SKILL.md", "---\nname: unchanged\ndescription: unchanged\n---\n")
	testkit.WriteFile(t, tempDir, "skills/not-a-skill.txt", "not a skill directory\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
targets = ["codex"]
	`)
	oldRemovedHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/removed"))
	var initialStdout bytes.Buffer
	var initialStderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &initialStdout, &initialStderr); exitCode != 0 {
		t.Fatalf("initial lock exitCode = %d, stdout = %q stderr = %q", exitCode, initialStdout.String(), initialStderr.String())
	}

	testkit.WriteFile(t, tempDir, "skills/added/SKILL.md", "---\nname: added\ndescription: added\n---\n")
	testkit.WriteFile(t, tempDir, "skills/changed/SKILL.md", "---\nname: changed\ndescription: changed\n---\n")
	if err := os.RemoveAll(filepath.Join(tempDir, "skills/removed")); err != nil {
		t.Fatalf("RemoveAll removed skill returned error: %v", err)
	}
	addedHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/added"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	for _, want := range []string{
		"lockfile changes: added=2 changed=1 removed=2 unchanged=3",
		`  - resource/skill/added entity="skill:added"`,
		`content_hash="` + addedHash + `"`,
		`  - resource/skill/changed changed=exact_supply,derivation`,
		`  - resource/skill/removed entity="skill:removed"`,
		`content_hash="` + oldRemovedHash + `"`,
		"  - resource/skill/unchanged",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunLockDryRunJSONReportsStructuredDelta(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, lockfilePath, hashes := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var payload clijson.Lock
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v\nstdout = %s", err, stdout.String())
	}
	if payload.SchemaVersion != 3 || payload.Command != "lock" || payload.Mode != "dry-run" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.ManifestPath != manifestPath || payload.LockfilePath != lockfilePath || !payload.PreviousFound {
		t.Fatalf("payload paths/previous = %#v", payload)
	}
	if payload.EntryCounts.Subjects != 6 {
		t.Fatalf("entry counts = %#v", payload.EntryCounts)
	}
	if payload.ChangeCounts.Added != 2 || payload.ChangeCounts.Changed != 1 || payload.ChangeCounts.Removed != 2 || payload.ChangeCounts.Unchanged != 3 {
		t.Fatalf("change counts = %#v", payload.ChangeCounts)
	}
	if !payload.HasChanges {
		t.Fatalf("HasChanges = false, want true")
	}

	changes := make(map[string]struct {
		status string
		before string
		after  string
	}, len(payload.SubjectChanges))
	for _, change := range payload.SubjectChanges {
		row := struct {
			status string
			before string
			after  string
		}{status: change.Status}
		if change.Before != nil && change.Before.ExactSupply != nil {
			row.before = change.Before.ExactSupply.ContentHash
		}
		if change.After != nil && change.After.ExactSupply != nil {
			row.after = change.After.ExactSupply.ContentHash
		}
		changes[change.Subject.Namespace+"/"+change.Subject.Name] = row
	}
	if changes["skill/review"].status != "added" || changes["skill/review"].after != hashes.Review {
		t.Fatalf("skill/review change = %#v", changes["skill/review"])
	}
	if changes["skill/oracle"].status != "changed" || changes["skill/oracle"].before != testkit.FixtureHash("old-oracle") || changes["skill/oracle"].after != hashes.Oracle {
		t.Fatalf("skill/oracle change = %#v", changes["skill/oracle"])
	}
	if changes["skill/legacy"].status != "removed" || changes["skill/legacy"].before != testkit.FixtureHash("legacy") {
		t.Fatalf("skill/legacy change = %#v", changes["skill/legacy"])
	}
	if changes["instructions/project"].status != "unchanged" || changes["instructions/project"].before != hashes.Instructions || changes["instructions/project"].after != hashes.Instructions {
		t.Fatalf("instructions/project change = %#v", changes["instructions/project"])
	}
}

func TestRunLockWriteReportsDeltaAndWritesExactLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, lockfilePath, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"wrote lockfile: " + lockfilePath,
		"lockfile changes: added=2 changed=1 removed=2 unchanged=3",
		"lockfile.subject.added:",
		"lockfile.subject.changed:",
		"lockfile.subject.removed:",
		"lockfile.subject.unchanged:",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}

	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 2 || len(testkit.LockedInstructions(t, locked)) != 1 || len(testkit.LockedHooks(t, locked)) != 0 {
		t.Fatalf("locked counts = skills %d instructions %d hooks %d", len(testkit.LockedSkills(t, locked)), len(testkit.LockedInstructions(t, locked)), len(testkit.LockedHooks(t, locked)))
	}
}
