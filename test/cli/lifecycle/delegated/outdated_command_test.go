package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestRunOutdatedReportsChangesWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, lockfilePath, _ := testkit.WriteLockDeltaFixture(t, tempDir)
	beforeLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"outdated", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"outdated: lockfile can be refreshed: " + lockfilePath,
		"lockfile changes: added=2 changed=1 removed=2 unchanged=3",
		"lockfile.subject.added:",
		"lockfile.subject.changed:",
		"lockfile.subject.removed:",
		"lockfile.subject.unchanged:",
		"next: run " + testkit.ExpectedShellCommand(t, "daem", "lock", "--manifest", manifestPath),
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	afterLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(afterLockfile, beforeLockfile) {
		t.Fatalf("outdated changed lockfile bytes")
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("outdated created .daem or stat failed unexpectedly: %v", err)
	}
}

func TestRunOutdatedCheckJSONExitsNonZeroWhenLockWouldChange(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"outdated", "--manifest", manifestPath, "--check", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var payload clijson.Lock
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v\nstdout = %s", err, stdout.String())
	}
	if payload.Command != "outdated" || payload.Mode != "check" {
		t.Fatalf("payload command/mode = %s/%s", payload.Command, payload.Mode)
	}
	if payload.ChangeCounts.Added != 2 || payload.ChangeCounts.Changed != 1 || payload.ChangeCounts.Removed != 2 || payload.ChangeCounts.Unchanged != 3 {
		t.Fatalf("change counts = %#v", payload.ChangeCounts)
	}
	if !payload.HasChanges {
		t.Fatalf("HasChanges = false, want true")
	}
}

func TestRunOutdatedCheckExitsZeroWhenLockIsCurrent(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, lockfilePath, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	lockExitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr)
	if lockExitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q", lockExitCode, lockStderr.String())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"outdated", "--manifest", manifestPath, "--check"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "outdated: lockfile is current: "+lockfilePath) {
		t.Fatalf("stdout = %q, want current lockfile message", stdout.String())
	}
}

func TestRunOutdatedWithClaudeExtensionReportsLockRefreshOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeGlobalExtensionManifest())

	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	lockExitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr)
	if lockExitCode != 0 {
		t.Fatalf("lock exitCode = %d, stdout = %q, stderr = %q", lockExitCode, lockStdout.String(), lockStderr.String())
	}
	beforeLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"outdated", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "outdated: lockfile is current: "+lockfilePath) {
		t.Fatalf("stdout = %q, want current lockfile message", stdout.String())
	}
	assertNoClaudePluginUpdateClaims(t, stdout.String())
	afterLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(afterLockfile, beforeLockfile) {
		t.Fatalf("outdated changed lockfile bytes")
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"outdated", "--manifest", manifestPath, "--check", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("json exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("json stderr = %q, want empty", stderr.String())
	}
	var payload clijson.Lock
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v\nstdout = %s", err, stdout.String())
	}
	if payload.Command != "outdated" || payload.Mode != "check" || payload.HasChanges {
		t.Fatalf("payload command/mode/has_changes = %s/%s/%v", payload.Command, payload.Mode, payload.HasChanges)
	}
	if payload.EntryCounts.Subjects != 1 || payload.ChangeCounts.Unchanged != 1 {
		t.Fatalf("payload counts = entries %#v changes %#v, want one unchanged subject", payload.EntryCounts, payload.ChangeCounts)
	}
	assertNoClaudePluginUpdateClaims(t, stdout.String())
}

func TestRunOutdatedWithChangedClaudeExtensionSourceReportsLockDeltaOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeGlobalExtensionManifest())

	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	lockExitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr)
	if lockExitCode != 0 {
		t.Fatalf("lock exitCode = %d, stdout = %q, stderr = %q", lockExitCode, lockStdout.String(), lockStderr.String())
	}
	beforeLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	changedManifest := strings.Replace(
		claudeGlobalExtensionManifest(),
		`source = { marketplace = "context7@market" }`,
		`source = { marketplace = "context8@market" }`,
		1,
	)
	if changedManifest == claudeGlobalExtensionManifest() {
		t.Fatalf("changed manifest fixture did not change the marketplace source")
	}
	testkit.WriteFile(t, tempDir, "daem.toml", changedManifest)
	canary := execcheck.New(t, "claude")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"outdated", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"outdated: lockfile can be refreshed: " + lockfilePath,
		"lockfile changes: added=0 changed=1 removed=0 unchanged=0",
		"next: run " + testkit.ExpectedShellCommand(t, "daem", "lock", "--manifest", manifestPath),
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	assertNoClaudePluginUpdateClaims(t, stdout.String())
	afterLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(afterLockfile, beforeLockfile) {
		t.Fatalf("outdated changed lockfile bytes")
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("outdated created .daem or stat failed unexpectedly: %v", err)
	}
	execcheck.AssertClean(t, canary, "outdated changed Claude extension source")

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"outdated", "--manifest", manifestPath, "--check", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("json exitCode = %d, want 1; stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("json stderr = %q, want empty", stderr.String())
	}
	var payload clijson.Lock
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v\nstdout = %s", err, stdout.String())
	}
	if payload.Command != "outdated" || payload.Mode != "check" || !payload.HasChanges {
		t.Fatalf("payload command/mode/has_changes = %s/%s/%v", payload.Command, payload.Mode, payload.HasChanges)
	}
	if payload.EntryCounts.Subjects != 1 || payload.ChangeCounts.Changed != 1 || len(payload.SubjectChanges) != 1 {
		t.Fatalf("payload counts = entries %#v changes %#v subject_changes=%d, want one changed subject", payload.EntryCounts, payload.ChangeCounts, len(payload.SubjectChanges))
	}
	change := payload.SubjectChanges[0]
	if change.Status != "changed" || change.Before == nil || change.After == nil ||
		change.Before.Realization == nil || change.After.Realization == nil {
		t.Fatalf("subject change = %#v, want changed delegated host relation", change)
	}
	if change.Before.Realization.CanonicalRequestHash == "" ||
		change.After.Realization.CanonicalRequestHash == "" ||
		change.Before.Realization.CanonicalRequestHash == change.After.Realization.CanonicalRequestHash {
		t.Fatalf(
			"canonical request hashes = %q -> %q, want a source-sensitive identity change",
			change.Before.Realization.CanonicalRequestHash,
			change.After.Realization.CanonicalRequestHash,
		)
	}
	assertNoClaudePluginUpdateClaims(t, stdout.String())
	execcheck.AssertClean(t, canary, "outdated check changed Claude extension source")
}
