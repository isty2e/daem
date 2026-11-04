//go:build darwin || linux

package commit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPermissionInspectionSharesQualificationWithoutMutationOrSync(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	writePermissionFixture(t, path, "malformed payload [[[", 0o644)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	captured := captureRootForCommitTest(t, root)
	request := permissionRequestForTest(t, captured, "config", 0o600)
	effects := 0
	observation, err := inspectOrRestrictRootedPermissions(t.Context(), request, faultPlan{actions: map[phase]func(){phaseApplyMode: func() { effects++ }, phaseSyncPayload: func() { effects++ }, phaseSyncCleanupParent: func() { effects++ }}}, false)
	if err != nil || !observation.Expanded() || observation.Restricted() || effects != 0 {
		t.Fatalf("inspection=%+v effects=%d err=%v", observation, effects, err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("inspection changed the file", err)
	}
	if err := os.Chmod(path, 0o744); err != nil {
		t.Fatal(err)
	}
	request = permissionRequestForTest(t, captured, "config", 0o600)
	if _, err := InspectRootedFilePermissions(t.Context(), request); err == nil {
		t.Fatal("inspection did not reuse the owner/execute-bit contract")
	}
}

func TestPermissionInspectionObservesMissingAncestorsWithoutCreatingThem(t *testing.T) {
	root := canonicalTempDir(t)
	captured := captureRootForCommitTest(t, root)
	path := filepath.Join(root, "missing", "config")
	request, err := NewRootedPermissionRestriction(rootedCapabilityForCommitTest(t, captured, "missing/config"), permissionPolicyForTest(t, path, uint32(os.Getuid()), uint32(os.Getgid()), 0o600))
	if err != nil {
		t.Fatal(err)
	}
	observation, err := InspectRootedFilePermissions(t.Context(), request)
	if err != nil || !observation.ObservedAbsent() || observation.Restricted() {
		t.Fatal("read-only absence did not qualify", err)
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatal("inspection created missing hierarchy", err)
	}
}
