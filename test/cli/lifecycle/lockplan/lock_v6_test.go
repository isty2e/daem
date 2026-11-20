package cli_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

const v6InstructionManifest = `version = 1
targets = ["codex"]

[instructions.project]
source = "source.md"
`

func TestReleasedV6LockKeepsPinsUntilExplicitRelock(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	manifest := filepath.Join(root, "daem.toml")
	lockPath := filepath.Join(root, "daem.lock.toml")
	const original = "project instructions\n"
	const changed = "project instructions changed since locking\n"
	testkit.WriteFile(t, root, "daem.toml", v6InstructionManifest)
	testkit.WriteFile(t, root, "source.md", original)
	// The fixture is the output of v0.7.2 lock for this manifest and source.
	legacy := string(testkit.ReadFile(t, "testdata/instructions-v6.lock.toml"))
	testkit.WriteFile(t, root, "daem.lock.toml", legacy)

	run := func(wantCode int, args ...string) string {
		t.Helper()
		args = append(args, "--manifest", manifest)
		code, stdout, stderr := testkit.RunCapturedCLI(args...)
		if code != wantCode {
			t.Fatalf("%v: exit = %d, want %d\nstdout: %s\nstderr: %s", args, code, wantCode, stdout, stderr)
		}
		return stdout
	}

	run(0, "apply", "--yes")
	testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), original)
	run(0, "status", "--check")
	unchanged := clijson.DecodeLock(t, []byte(run(0, "outdated", "--check", "--json")))
	if unchanged.PreviousStatus != "comparable" || unchanged.HasChanges == nil || *unchanged.HasChanges {
		t.Fatalf("unmodified v6 comparison: %+v", unchanged)
	}
	testkit.AssertFileContent(t, lockPath, legacy)

	testkit.WriteFile(t, root, "source.md", changed)
	run(1, "apply", "--yes")
	testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), original)
	stale := clijson.DecodeLock(t, []byte(run(1, "outdated", "--check", "--json")))
	if stale.PreviousStatus != "comparable" || stale.HasChanges == nil || !*stale.HasChanges {
		t.Fatalf("changed source v6 comparison: %+v", stale)
	}
	requireUnavailableLockComparison(t, run(0, "lock", "--dry-run", "--json"), "legacy", "dry-run")
	testkit.AssertFileContent(t, lockPath, legacy)

	requireUnavailableLockComparison(t, run(0, "lock", "--json"), "legacy", "write")
	if _, err := lockfile.DecodeForRelock(t.Context(), testkit.ReadFile(t, lockPath)); err != nil {
		t.Fatalf("the explicit relock did not write the current schema: %v", err)
	}
	testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), original)
	run(0, "apply", "--yes")
	testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), changed)
}

func TestUnusableV6LockPointsToExplicitRegeneration(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	testkit.WriteFile(t, root, "daem.toml", v6InstructionManifest)
	testkit.WriteFile(t, root, "source.md", "project instructions\n")
	const damaged = "version = 6\n[[\n"
	testkit.WriteFile(t, root, "daem.lock.toml", damaged)

	manifest := filepath.Join(root, "daem.toml")
	lockPath := filepath.Join(root, "daem.lock.toml")
	code, stdout, stderr := testkit.RunCapturedCLI("apply", "--dry-run", "--json", "--manifest", manifest)
	if code != 1 {
		t.Fatalf("apply exited %d, stdout=%s stderr=%s", code, stdout, stderr)
	}
	failure := clijson.DecodeFailure(t, stdout, "apply", "operation")
	if len(failure.Guidance) != 1 || failure.Guidance[0].Kind != "instruction" ||
		!strings.Contains(failure.Guidance[0].Message, "daem lock --dry-run") {
		t.Fatalf("unusable v6 recovery guidance: %+v", failure.Guidance)
	}
	testkit.AssertFileContent(t, lockPath, damaged)

	preview := runLockReasonCLI(t, "lock", "--dry-run", "--json", "--manifest", manifest)
	requireUnavailableLockComparison(t, string(preview), "legacy", "dry-run")
	testkit.AssertFileContent(t, lockPath, damaged)
	written := runLockReasonCLI(t, "lock", "--json", "--manifest", manifest)
	requireUnavailableLockComparison(t, string(written), "legacy", "write")
	if _, err := lockfile.DecodeForRelock(t.Context(), testkit.ReadFile(t, lockPath)); err != nil {
		t.Fatalf("the explicit relock did not replace the damaged schema 6 lock: %v", err)
	}
}
