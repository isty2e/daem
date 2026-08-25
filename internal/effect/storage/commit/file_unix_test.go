//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/unix"
)

func TestCommitFileCreateAndReplace(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "nested", "state.json")
	payload := []byte("created")
	create, err := NewFileCreate(target, payload, 0o640)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	payload[0] = 'X'
	if err := CommitFile(context.Background(), create); err != nil {
		t.Fatalf("CommitFile create returned error: %v", err)
	}
	assertFile(t, target, "created", 0o640)

	identity := captureIdentity(t, target)
	replace, err := NewFileReplacement(target, []byte("replaced"), 0o600, identity)
	if err != nil {
		t.Fatalf("NewFileReplacement returned error: %v", err)
	}
	if err := CommitFile(context.Background(), replace); err != nil {
		t.Fatalf("CommitFile replacement returned error: %v", err)
	}
	assertFile(t, target, "replaced", 0o600)
	assertNoPrivateEntries(t, filepath.Dir(target))
}

func TestCommitFileRejectsExistingAndStaleEntries(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	writeTestFile(t, target, "old", 0o600)

	create, err := NewFileCreate(target, []byte("new"), 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	assertFailure(t, CommitFile(context.Background(), create), failureUncommitted, phaseValidate)
	assertFile(t, target, "old", 0o600)

	identity := captureIdentity(t, target)
	replace, err := NewFileReplacement(target, []byte("new"), 0o600, identity)
	if err != nil {
		t.Fatalf("NewFileReplacement returned error: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	writeTestFile(t, target, "external", 0o600)
	assertFailure(t, CommitFile(context.Background(), replace), failureUncommitted, phaseValidate)
	assertFile(t, target, "external", 0o600)
}

func TestStorageCommitModelsRejectIncompatibleIdentities(t *testing.T) {
	root := canonicalTempDir(t)
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	directoryIdentity := captureIdentity(t, directory)
	if _, err := NewFileReplacement(directory, nil, 0o600, directoryIdentity); err == nil {
		t.Fatal("NewFileReplacement accepted directory identity")
	}
	if _, err := NewLogicalRemoval(filepath.Join(root, "other-entry"), directoryIdentity); err == nil {
		t.Fatal("NewLogicalRemoval accepted identity for another path")
	}
}

func TestEntryIdentitySeparatesIncarnationFromOpenedObject(t *testing.T) {
	first := EntryIdentity{
		path: "/tmp/entry",
		kind: entryKindRegular,
		platform: platformIdentity{
			device:           1,
			inode:            2,
			changeTimeSecond: 3,
			changeTimeNano:   4,
		},
	}
	changed := first
	changed.platform.changeTimeNano++
	if first.sameEntry(changed) {
		t.Fatal("sameEntry accepted a changed incarnation")
	}
	if !first.sameObject(changed) {
		t.Fatal("sameObject rejected the still-open device/inode object")
	}
	changed.kind = entryKindDirectory
	if first.sameObject(changed) {
		t.Fatal("sameObject accepted a different entry kind")
	}
}

func TestCaptureEntryIdentityRejectsMissingAndSpecialEntries(t *testing.T) {
	root := canonicalTempDir(t)
	missing := filepath.Join(root, "missing")
	assertFailure(t, captureIdentityError(missing), failureUncommitted, phaseCaptureIdentity)

	fifo := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("Mkfifo returned error: %v", err)
	}
	assertFailure(t, captureIdentityError(fifo), failureUnsupportedGuarantee, phaseCaptureIdentity)
}

func TestStorageCommitZeroValuesFailBeforeEffects(t *testing.T) {
	assertFailure(t, CommitFile(context.Background(), FileCommit{}), failureUncommitted, phaseValidate)
	assertFailure(t, CommitLogicalRemoval(context.Background(), LogicalRemoval{}), failureUncommitted, phaseValidate)
}

func TestCommitFileReplacementHasNamedEntryHardLinkSemantics(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	sibling := filepath.Join(root, "state.link")
	writeTestFile(t, target, "old", 0o600)
	if err := os.Link(target, sibling); err != nil {
		t.Fatalf("Link returned error: %v", err)
	}

	request, err := NewFileReplacement(target, []byte("new"), 0o600, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewFileReplacement returned error: %v", err)
	}
	if err := CommitFile(context.Background(), request); err != nil {
		t.Fatalf("CommitFile returned error: %v", err)
	}
	assertFile(t, target, "new", 0o600)
	assertFile(t, sibling, "old", 0o600)
}

func TestValidateOwnedStatRejectsForeignOwner(t *testing.T) {
	stat := unix.Stat_t{Uid: uint32(unix.Geteuid() + 1)}
	if err := validateOwnedStat("/foreign", &stat); !isUnsupported(err) {
		t.Fatalf("validateOwnedStat error = %v, want unsupported", err)
	}
}

func TestCommitFileHonorsCancellationAndPermissions(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "cancelled")
	request, err := NewFileCreate(target, []byte("payload"), 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertFailure(t, CommitFile(ctx, request), failureUncommitted, phaseValidate)

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	defer os.Chmod(locked, 0o700)
	permissionTarget := filepath.Join(locked, "state.json")
	permissionRequest, err := NewFileCreate(permissionTarget, nil, 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	assertFailure(t, CommitFile(context.Background(), permissionRequest), failureUncommitted, phaseCreateTemporary)
}

func assertFile(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", path, err)
	}
	if string(payload) != content {
		t.Fatalf("content = %q, want %q", payload, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) returned error: %v", path, err)
	}
	if info.Mode().Perm() != mode.Perm() {
		t.Fatalf("mode = %04o, want %04o", info.Mode().Perm(), mode.Perm())
	}
}

func captureIdentity(t *testing.T, path string) EntryIdentity {
	t.Helper()
	identity, err := CaptureEntryIdentity(context.Background(), path)
	if err != nil {
		t.Fatalf("CaptureEntryIdentity(%q) returned error: %v", path, err)
	}
	return identity
}

func captureIdentityError(path string) error {
	_, err := CaptureEntryIdentity(context.Background(), path)
	return err
}

func writeTestFile(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", path, err)
	}
}

func assertFailure(t *testing.T, err error, kind mutationfs.FailureKind, failedPhase phase) *failure {
	t.Helper()
	var failure *failure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want *failure", err)
	}
	if failure.Kind() != kind {
		t.Fatalf("failure kind = %s, want %s: %v", failure.Kind(), kind, failure)
	}
	if failure.failedPhase() != string(failedPhase) {
		t.Fatalf("failure phase = %s, want %s: %v", failure.failedPhase(), failedPhase, failure)
	}
	return failure
}

func assertNoPrivateEntries(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir(%q) returned error: %v", directory, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), temporaryPrefix) || strings.HasPrefix(entry.Name(), tombstonePrefix) {
			t.Fatalf("unexpected private entry %q", filepath.Join(directory, entry.Name()))
		}
	}
}

func faultAt(failedPhase phase) faultPlan {
	return faultPlan{failures: map[phase]error{failedPhase: errors.New("injected " + string(failedPhase))}}
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) returned error: %v", path, err)
	}
	return canonical
}
