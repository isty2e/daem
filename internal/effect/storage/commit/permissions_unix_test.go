//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"golang.org/x/sys/unix"
)

func permissionRequestForTest(t *testing.T, captured *rootedpath.CapturedRoot, relative string, capMode fs.FileMode) RootedPermissionRestriction {
	t.Helper()
	capability := rootedCapabilityForCommitTest(t, captured, relative)
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	request, err := NewRootedPermissionRestriction(capability, permissionPolicyForTest(t, path, stat.Uid, stat.Gid, capMode))
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	return request
}

func permissionPolicyForTest(t *testing.T, path string, uid, gid uint32, mode fs.FileMode) PermissionRestrictionPolicy {
	t.Helper()
	statePath := filepath.Join(filepath.Dir(path), ".owner", "state.json")
	stateKey, err := pathauthority.NewExact(statePath, "exact-v1:")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(stateKey, filepath.Join(filepath.Dir(statePath), "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	physical, err := pathauthority.NewExact(path, "exact-v1:")
	if err != nil {
		t.Fatal(err)
	}
	logical, err := output.Parse("@pi-agent/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(owner, logical, physical)
	if err != nil {
		t.Fatal(err)
	}
	document, err := ownership.NewProtectedDocument([]ownership.DocumentBinding{binding}, uid, gid, uint32(mode))
	if err != nil {
		t.Fatal(err)
	}
	return document.Restriction
}

func requirePermissionFailure(t *testing.T, err error, kind mutationfs.FailureKind) {
	t.Helper()
	var failure *failure
	if !errors.As(err, &failure) || failure.Kind() != kind {
		t.Fatalf("permission failure classification = %v, want %s", err, kind)
	}
}

func writePermissionFixture(t *testing.T, path string, content string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestPermissionRestrictionIntersectsWithoutRestoringOrReadingContent(t *testing.T) {
	for _, test := range []struct {
		capMode, current, want fs.FileMode
		expanded               bool
	}{
		{0o600, 0o644, 0o600, true},
		{0o640, 0o604, 0o600, true},
		{0o644, 0o600, 0o600, false},
		{0o640, 0o600, 0o600, false},
		{0o640, 0o660, 0o640, true},
		{0o400, 0o400, 0o400, false},
	} {
		t.Run(fmt.Sprintf("%o-to-%o", test.current, test.capMode), func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "config")
			payload := "not valid TOML [[[\n\x00private fixture"
			writePermissionFixture(t, path, payload, test.current)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			captured := captureRootForCommitTest(t, root)
			request := permissionRequestForTest(t, captured, "config", test.capMode)
			chmodCalls := 0
			result, err := restrictRootedFilePermissions(t.Context(), request, faultPlan{
				actions: map[phase]func(){phaseApplyMode: func() { chmodCalls++ }},
			})
			if err != nil || result.Expanded() != test.expanded || result.Restricted() != test.expanded {
				t.Fatal("permission observation/restriction mismatch", err)
			}
			if chmodCalls != 0 && !test.expanded {
				t.Fatal("already-contained mode reached chmod")
			}
			after, err := os.Stat(path)
			if err != nil || !os.SameFile(before, after) || after.Mode().Perm() != test.want || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("restriction replaced or rewrote its file", err)
			}
			assertFileContent(t, path, payload)
			assertClosedRootedCapability(t, request.capability)
		})
	}
}

func TestPermissionRestrictionRefusesUnqualifiedMetadataAndAliases(t *testing.T) {
	for _, mode := range []string{"owner bits", "execute bits", "recorded uid", "recorded gid", "hard link", "symlink", "special bits"} {
		t.Run(mode, func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "config")
			writePermissionFixture(t, path, "unchanged", 0o644)
			if mode == "hard link" {
				if err := os.Link(path, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "owner bits" {
				if err := os.Chmod(path, 0o444); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "execute bits" {
				if err := os.Chmod(path, 0o645); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "special bits" {
				if err := unix.Chmod(path, 0o1644); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "symlink" {
				if err := os.Rename(path, filepath.Join(root, "target")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target", path); err != nil {
					t.Fatal(err)
				}
			}
			request := permissionRequestForTest(t, captureRootForCommitTest(t, root), "config", 0o600)
			var metadata unix.Stat_t
			if err := unix.Stat(path, &metadata); err != nil {
				t.Fatal(err)
			}
			if mode == "recorded uid" {
				request.policy = permissionPolicyForTest(t, path, metadata.Uid+1, metadata.Gid, 0o600)
			}
			if mode == "recorded gid" {
				request.policy = permissionPolicyForTest(t, path, metadata.Uid, metadata.Gid+1, 0o600)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := RestrictRootedFilePermissions(t.Context(), request)
			if err == nil || result.Restricted() {
				t.Fatal("unqualified target reached permission mutation")
			}
			after, statErr := os.Stat(path)
			if statErr != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("refusal changed its target", statErr)
			}
			assertFileContent(t, path, "unchanged")
		})
	}
}

func TestPermissionRestrictionReportsVisibleSyncFailureAndRetriesDurability(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	writePermissionFixture(t, path, "malformed {", 0o644)
	captured := captureRootForCommitTest(t, root)
	request := permissionRequestForTest(t, captured, "config", 0o600)
	result, err := restrictRootedFilePermissions(t.Context(), request, faultAt(phaseSyncPayload))
	requirePermissionFailure(t, err, failureIndeterminateCommit)
	if !result.Expanded() || !result.Restricted() {
		t.Fatal("visible restriction prefix was hidden after failed sync")
	}
	assertFile(t, path, "malformed {", 0o600)
	request = permissionRequestForTest(t, captured, "config", 0o600)
	result, err = restrictRootedFilePermissions(t.Context(), request, faultAt(phaseSyncPayload))
	if err == nil || result.Expanded() || result.Restricted() {
		t.Fatal("secure-looking retry skipped its durability obligation", err)
	}
	request = permissionRequestForTest(t, captured, "config", 0o600)
	result, err = RestrictRootedFilePermissions(t.Context(), request)
	if err != nil || result.Expanded() || result.Restricted() {
		t.Fatal("fresh durable no-op did not settle without restoring rights", err)
	}
	assertFile(t, path, "malformed {", 0o600)
}

func TestPermissionRestrictionNeverSettlesADisplacedDescriptor(t *testing.T) {
	for _, currentPhase := range []phase{phaseApplyMode, phaseVerifyEntry} {
		t.Run(string(currentPhase), func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "config")
			displaced := filepath.Join(root, "old")
			writePermissionFixture(t, path, "original", 0o644)
			request := permissionRequestForTest(t, captureRootForCommitTest(t, root), "config", 0o600)
			result, err := restrictRootedFilePermissions(t.Context(), request, faultPlan{
				actions: map[phase]func(){currentPhase: func() {
					if err := os.Rename(path, displaced); err != nil {
						t.Fatal(err)
					}
					writePermissionFixture(t, path, "replacement", 0o666)
				}},
			})
			if currentPhase == phaseApplyMode {
				requirePermissionFailure(t, err, failureUncommitted)
				if result.Restricted() {
					t.Fatal("pre-effect swap reached chmod")
				}
				assertFile(t, displaced, "original", 0o644)
			} else {
				requirePermissionFailure(t, err, failureIndeterminateCommit)
				if !result.Restricted() {
					t.Fatal("post-effect swap lost its visible prefix")
				}
				assertFile(t, displaced, "original", 0o600)
			}
			assertFile(t, path, "replacement", 0o666)
		})
	}
}

func TestPermissionRestrictionHonorsItsSuppliedSafetyContext(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	writePermissionFixture(t, path, "unchanged", 0o644)
	request := permissionRequestForTest(t, captureRootForCommitTest(t, root), "config", 0o600)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := RestrictRootedFilePermissions(ctx, request)
	if !errors.Is(err, context.Canceled) || result.Restricted() {
		t.Fatal("restriction invented a new cleanup allowance", err)
	}
	assertFile(t, path, "unchanged", 0o644)
}

func TestPermissionRestrictionRejectsPoliciesOutsideItsPhysicalEnvelope(t *testing.T) {
	for _, test := range []struct {
		mode     uint32
		expanded bool
	}{{0o666, true}, {0o444, true}, {0o605, true}, {0o600, false}} {
		root := canonicalTempDir(t)
		path := filepath.Join(root, "config")
		writePermissionFixture(t, path, "unchanged", 0o644)
		request := permissionRequestForTest(t, captureRootForCommitTest(t, root), "config", 0o600)
		request.policy = func(uint32, uint32, uint32) (uint32, bool, error) {
			return test.mode, test.expanded, nil
		}
		result, err := RestrictRootedFilePermissions(t.Context(), request)
		requirePermissionFailure(t, err, failureUnsupportedGuarantee)
		if result.Restricted() {
			t.Fatal("granting or inconsistent policy reached chmod")
		}
		assertFile(t, path, "unchanged", 0o644)
	}
}
