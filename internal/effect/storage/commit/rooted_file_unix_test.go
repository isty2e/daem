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
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestCaptureWorkingDirectoryIdentityUsesRetainedRoot(t *testing.T) {
	rootPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize retained root: %v", err)
	}
	expected, err := CaptureEntryIdentity(t.Context(), rootPath)
	if err != nil {
		t.Fatalf("capture ambient directory identity: %v", err)
	}
	root, err := rootedpath.CaptureRootNoFollow(rootPath)
	if err != nil {
		t.Fatalf("capture retained root: %v", err)
	}
	defer root.Close()
	budget := &workingDirectoryIdentityTestBudget{}
	capability, err := root.AcquireWorkingDirectoryBounded(budget)
	if err != nil {
		t.Fatalf("acquire retained working directory: %v", err)
	}
	defer capability.Close()
	observed, err := CaptureWorkingDirectoryIdentity(t.Context(), capability, budget)
	if err != nil {
		t.Fatalf("capture retained directory identity: %v", err)
	}
	if !expected.Equal(observed) {
		t.Fatal("retained directory identity differs from ambient observation")
	}
}

func TestCaptureWorkingDirectoryIdentityPreservesPreexistingCancellation(t *testing.T) {
	rootPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize retained root: %v", err)
	}
	root, err := rootedpath.CaptureRootNoFollow(rootPath)
	if err != nil {
		t.Fatalf("capture retained root: %v", err)
	}
	defer root.Close()
	acquireBudget := &workingDirectoryIdentityTestBudget{}
	capability, err := root.AcquireWorkingDirectoryBounded(acquireBudget)
	if err != nil {
		t.Fatalf("acquire retained working directory: %v", err)
	}
	defer capability.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	observationBudget := &workingDirectoryIdentityTestBudget{}
	_, err = CaptureWorkingDirectoryIdentity(ctx, capability, observationBudget)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retained identity error = %v, want context.Canceled", err)
	}
	if observationBudget.calls != 0 {
		t.Fatalf("canceled retained identity consumed %d path-budget calls", observationBudget.calls)
	}
}

type workingDirectoryIdentityTestBudget struct {
	calls int
}

func (budget *workingDirectoryIdentityTestBudget) AdmitPathComponents(int) error {
	budget.calls++
	return nil
}

func TestMissingRootedEntryPreservesNotExistCause(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	capability := rootedCapabilityForCommitTest(t, captureRootForCommitTest(t, root), ".agents/config")
	defer capability.Close()

	_, err := CaptureRootedEntryIdentity(context.Background(), capability)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CaptureRootedEntryIdentity error = %T %v, want errors.Is(os.ErrNotExist)", err, err)
	}
}

func TestConfirmRootedEntryAbsentSyncsNearestExistingAncestorWithoutCreatingParents(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "missing/nested/.daem-tombstone-residue")
	if _, err := ConfirmRootedEntryAbsentWithOutcome(t.Context(), capability); err != nil {
		t.Fatalf("ConfirmRootedEntryAbsent returned error: %v", err)
	}
	assertClosedRootedCapability(t, capability)
	if _, err := os.Stat(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing parent stat error = %v, want not exist", err)
	}
}

func TestConfirmRootedEntryAbsentRejectsReappearedResidue(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	residue := filepath.Join(root, ".daem-tombstone-residue")
	if err := os.WriteFile(residue, []byte("foreign"), 0o600); err != nil {
		t.Fatalf("create residue: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, filepath.Base(residue))
	_, err := ConfirmRootedEntryAbsentWithOutcome(t.Context(), capability)
	if err == nil || !strings.Contains(err.Error(), "reappeared") {
		t.Fatalf("ConfirmRootedEntryAbsent error = %v, want reappeared blocker", err)
	}
	assertFile(t, residue, "foreign", 0o600)
}

func TestConfirmRootedEntryAbsentRejectsSymlinkAncestor(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatalf("create outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "missing")); err != nil {
		t.Fatalf("create ancestor symlink: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "missing/residue")
	_, err := ConfirmRootedEntryAbsentWithOutcome(t.Context(), capability)
	if err == nil || !strings.Contains(err.Error(), "symbolic-link ancestor") {
		t.Fatalf("ConfirmRootedEntryAbsent error = %v, want symlink blocker", err)
	}
}

func TestConfirmRootedEntryAbsentHonorsCancellation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "residue")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := ConfirmRootedEntryAbsentWithOutcome(ctx, capability)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ConfirmRootedEntryAbsent error = %v, want context cancellation", err)
	}
	assertClosedRootedCapability(t, capability)
}

func TestConfirmRootedEntryAbsentRetriesAfterDurabilityFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	first := rootedCapabilityForCommitTest(t, captured, "missing/residue")
	faults := faultPlan{failures: map[phase]error{
		phaseSyncCleanupParent: errors.New("injected absence sync failure"),
	}}
	if err := confirmRootedEntryAbsentWithFaults(t.Context(), first, faults); err == nil || !strings.Contains(err.Error(), "injected absence sync failure") {
		t.Fatalf("first absence confirmation error = %v, want injected failure", err)
	}
	second := rootedCapabilityForCommitTest(t, captured, "missing/residue")
	if _, err := ConfirmRootedEntryAbsentWithOutcome(t.Context(), second); err != nil {
		t.Fatalf("retry absence confirmation returned error: %v", err)
	}
}

func TestRootedFileCommitCreateReplaceAndRemoveStayRooted(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	destination := ".agents/skills/review/SKILL.md"
	hostPath := filepath.Join(root, filepath.FromSlash(destination))

	createCapability := rootedCapabilityForCommitTest(t, captured, destination)
	create, err := NewRootedFileCreate(createCapability, []byte("first"), 0o600)
	if err != nil {
		t.Fatalf("NewRootedFileCreate returned error: %v", err)
	}
	if err := CommitFile(context.Background(), create); err != nil {
		t.Fatalf("CommitFile(create) returned error: %v", err)
	}
	assertClosedRootedCapability(t, createCapability)
	assertFileContent(t, hostPath, "first")

	replaceCapability := rootedCapabilityForCommitTest(t, captured, destination)
	expected, err := CaptureRootedEntryIdentity(context.Background(), replaceCapability)
	if err != nil {
		t.Fatalf("CaptureRootedEntryIdentity returned error: %v", err)
	}
	replace, err := NewRootedFileReplacement(replaceCapability, []byte("second"), 0o640, expected)
	if err != nil {
		t.Fatalf("NewRootedFileReplacement returned error: %v", err)
	}
	if err := CommitFile(context.Background(), replace); err != nil {
		t.Fatalf("CommitFile(replace) returned error: %v", err)
	}
	assertClosedRootedCapability(t, replaceCapability)
	assertFileContent(t, hostPath, "second")

	removeCapability := rootedCapabilityForCommitTest(t, captured, destination)
	expected, err = CaptureRootedEntryIdentity(context.Background(), removeCapability)
	if err != nil {
		t.Fatalf("CaptureRootedEntryIdentity(remove) returned error: %v", err)
	}
	removal, err := NewRootedLogicalRemoval(removeCapability, expected)
	if err != nil {
		t.Fatalf("NewRootedLogicalRemoval returned error: %v", err)
	}
	if err := CommitLogicalRemoval(context.Background(), removal); err != nil {
		t.Fatalf("CommitLogicalRemoval returned error: %v", err)
	}
	assertClosedRootedCapability(t, removeCapability)
	if _, err := os.Lstat(hostPath); !os.IsNotExist(err) {
		t.Fatalf("removed path Lstat error = %v, want not exist", err)
	}
}

func TestReadRootedRegularFileSuppliesReplacementIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	writeTestFile(t, filepath.Join(root, ".agents", "config"), "before", 0o640)
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/config")
	content, mode, expected, err := ReadRootedRegularFile(context.Background(), capability)
	if err != nil {
		t.Fatalf("ReadRootedRegularFile returned error: %v", err)
	}
	if string(content) != "before" || mode != 0o640 {
		t.Fatalf("ReadRootedRegularFile = %q mode %o, want before mode 640", content, mode)
	}
	request, err := NewRootedFileReplacement(capability, []byte("after"), 0o600, expected)
	if err != nil {
		t.Fatalf("NewRootedFileReplacement returned error: %v", err)
	}
	if err := CommitFile(context.Background(), request); err != nil {
		t.Fatalf("CommitFile returned error: %v", err)
	}
	assertClosedRootedCapability(t, capability)
	assertFileContent(t, filepath.Join(root, ".agents", "config"), "after")
}

func TestRootedFileReplacementRefreshesOnlyExpectedParentIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	parentPath := filepath.Join(root, ".agents")
	if err := os.MkdirAll(parentPath, 0o700); err != nil {
		t.Fatalf("create rooted parent: %v", err)
	}
	recordPath := filepath.Join(parentPath, "config")
	writeTestFile(t, recordPath, "before", 0o600)
	captured := captureRootForCommitTest(t, root)
	adapter := Adapter{}

	captureParent := func() EntryIdentity {
		t.Helper()
		capability := rootedCapabilityForCommitTest(t, captured, ".agents")
		defer capability.Close()
		identity, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture parent identity: %v", err)
		}
		return identity
	}
	captureRecord := func(capability rootedpath.CommitCapability) EntryIdentity {
		t.Helper()
		identity, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture record identity: %v", err)
		}
		return identity
	}

	expectedParent := captureParent()
	replaceCapability := rootedCapabilityForCommitTest(t, captured, ".agents/config")
	expectedRecord := captureRecord(replaceCapability)
	outcome, refreshedParent, err := adapter.ReplaceRootedFileAndRefreshParent(
		t.Context(),
		replaceCapability,
		[]byte("after"),
		0o600,
		expectedRecord,
		expectedParent,
	)
	if err != nil {
		t.Fatalf("ReplaceRootedFileAndRefreshParent returned error: %v", err)
	}
	if outcome.State() != mutationfs.CommitOutcomeComplete {
		t.Fatalf("replacement outcome = %q, want complete", outcome.State())
	}
	currentParent := captureParent()
	if refreshedParent == nil || !refreshedParent.Equal(currentParent) {
		t.Fatal("replacement did not return the exact refreshed parent identity")
	}
	assertFileContent(t, recordPath, "after")

	staleParent := currentParent
	if err := os.WriteFile(filepath.Join(parentPath, "foreign"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	staleCapability := rootedCapabilityForCommitTest(t, captured, ".agents/config")
	staleRecord := captureRecord(staleCapability)
	outcome, refreshedParent, err = adapter.ReplaceRootedFileAndRefreshParent(
		t.Context(),
		staleCapability,
		[]byte("must-not-commit"),
		0o600,
		staleRecord,
		staleParent,
	)
	if err == nil || !strings.Contains(err.Error(), "parent directory identity changed") {
		t.Fatalf("stale parent replacement error = %v", err)
	}
	if outcome.State() != mutationfs.CommitOutcomeUncommitted {
		t.Fatalf("stale parent outcome = %q, want uncommitted", outcome.State())
	}
	if refreshedParent != nil {
		t.Fatal("stale parent replacement returned refreshed authority")
	}
	assertFileContent(t, recordPath, "after")
}

func TestReadRootedRegularFileUpToEnforcesPayloadBound(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	writeTestFile(t, filepath.Join(root, ".agents", "config"), "12345", 0o640)
	captured := captureRootForCommitTest(t, root)

	exact := rootedCapabilityForCommitTest(t, captured, ".agents/config")
	content, mode, _, err := ReadRootedRegularFileUpTo(t.Context(), exact, 5)
	if err != nil {
		t.Fatalf("ReadRootedRegularFileUpTo exact bound returned error: %v", err)
	}
	if string(content) != "12345" || mode != 0o640 {
		t.Fatalf("bounded rooted read = %q mode %o, want 12345 mode 640", content, mode)
	}
	if err := exact.Close(); err != nil {
		t.Fatalf("close exact-bound capability: %v", err)
	}

	oversized := rootedCapabilityForCommitTest(t, captured, ".agents/config")
	if _, _, _, err := ReadRootedRegularFileUpTo(t.Context(), oversized, 4); err == nil {
		t.Fatal("ReadRootedRegularFileUpTo oversized file returned nil error")
	}
	if err := oversized.Close(); err != nil {
		t.Fatalf("close oversized capability: %v", err)
	}

	invalid := rootedCapabilityForCommitTest(t, captured, ".agents/config")
	if _, _, _, err := ReadRootedRegularFileUpTo(t.Context(), invalid, 0); err == nil {
		t.Fatal("ReadRootedRegularFileUpTo zero bound returned nil error")
	}
	if err := invalid.Close(); err != nil {
		t.Fatalf("close invalid-bound capability: %v", err)
	}
}

func TestReadRootedSymlinkTargetReturnsStableNoFollowTarget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	linkPath := filepath.Join(root, ".agents", "current")
	if err := os.Symlink("nested/target", linkPath); err != nil {
		t.Fatalf("create rooted symbolic link: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/current")

	target, identity, err := ReadRootedSymlinkTarget(t.Context(), capability)
	if err != nil {
		t.Fatalf("ReadRootedSymlinkTarget returned error: %v", err)
	}
	if target != "nested/target" {
		t.Fatalf("symbolic-link target = %q, want nested/target", target)
	}
	if identity.kind != entryKindSymlink {
		t.Fatalf("symbolic-link identity kind = %d, want symlink", identity.kind)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close symbolic-link capability: %v", err)
	}

	writeTestFile(t, filepath.Join(root, ".agents", "regular"), "content", 0o600)
	regular := rootedCapabilityForCommitTest(t, captured, ".agents/regular")
	if _, _, err := ReadRootedSymlinkTarget(t.Context(), regular); err == nil ||
		!strings.Contains(err.Error(), "not a symbolic link") {
		t.Fatalf("regular-file symbolic-link read error = %v, want kind rejection", err)
	}
	if err := regular.Close(); err != nil {
		t.Fatalf("close regular-file capability: %v", err)
	}
}

func captureRootForCommitTest(t *testing.T, root string) *rootedpath.CapturedRoot {
	t.Helper()
	captured, err := rootedpath.CaptureRoot(root)
	if err != nil {
		t.Fatalf("CaptureRoot(%q) returned error: %v", root, err)
	}
	t.Cleanup(func() {
		if err := captured.Close(); err != nil {
			t.Errorf("close captured captured root: %v", err)
		}
	})
	return captured
}

func rootedCapabilityForCommitTest(
	t *testing.T,
	captured *rootedpath.CapturedRoot,
	relativePath string,
) rootedpath.CommitCapability {
	t.Helper()
	authority, err := captured.Authority()
	if err != nil {
		t.Fatalf("CapturedRoot.Authority returned error: %v", err)
	}
	relative, err := rootedpath.NewRelativeDestination(relativePath)
	if err != nil {
		t.Fatalf("NewRelativeDestination(%q) returned error: %v", relativePath, err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatalf("Authority.Bind returned error: %v", err)
	}
	capability, err := captured.Acquire(destination)
	if err != nil {
		t.Fatalf("CapturedRoot.Acquire returned error: %v", err)
	}
	return capability
}

func assertClosedRootedCapability(t *testing.T, capability rootedpath.CommitCapability) {
	t.Helper()
	if err := capability.Validate(); !hasRootedPathFailureKind(err, rootedpath.FailureRootUnavailable) {
		t.Fatalf("consumed capability Validate error = %v, want %s", err, rootedpath.FailureRootUnavailable)
	}
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("ReadFile(%q) = %q, want %q", path, content, want)
	}
}
