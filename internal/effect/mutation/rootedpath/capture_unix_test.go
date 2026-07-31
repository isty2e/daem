//go:build darwin || linux

package rootedpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureDestinationBindsMissingDescendantsToNearestExistingAncestor(t *testing.T) {
	ancestor := filepath.Join(t.TempDir(), "admitted")
	if err := os.Mkdir(ancestor, 0o700); err != nil {
		t.Fatalf("create admitted ancestor: %v", err)
	}
	selected := filepath.Join(ancestor, ".agents", "skills", "review")
	physicalAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		t.Fatalf("resolve admitted ancestor: %v", err)
	}
	want := filepath.Join(physicalAncestor, ".agents", "skills", "review")

	root, destination, err := CaptureDestination(selected)
	if err != nil {
		t.Fatalf("CaptureDestination returned error: %v", err)
	}
	defer root.Close()
	if got := destination.Root().PhysicalRoot(); got != physicalAncestor {
		t.Fatalf("captured physical root = %q, want %q", got, physicalAncestor)
	}
	if got := destination.Relative().Path(); got != ".agents/skills/review" {
		t.Fatalf("captured relative destination = %q, want %q", got, ".agents/skills/review")
	}
	if got, err := destination.LexicalPath(); err != nil || got != want {
		t.Fatalf("captured lexical destination = %q, error = %v, want %q", got, err, want)
	}
}

func TestCaptureDestinationRetainsPhysicalAncestorAfterAliasRetarget(t *testing.T) {
	parent := t.TempDir()
	admitted := filepath.Join(parent, "admitted")
	retargeted := filepath.Join(parent, "retargeted")
	for _, directory := range []string{admitted, retargeted} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create destination ancestor %q: %v", directory, err)
		}
	}
	alias := filepath.Join(parent, "selected")
	if err := os.Symlink(admitted, alias); err != nil {
		t.Fatalf("create selected ancestor alias: %v", err)
	}

	root, destination, err := CaptureDestination(filepath.Join(alias, "missing", "entry"))
	if err != nil {
		t.Fatalf("CaptureDestination returned error: %v", err)
	}
	defer root.Close()
	capability, err := root.Acquire(destination)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer capability.Close()

	if err := os.Remove(alias); err != nil {
		t.Fatalf("remove selected ancestor alias: %v", err)
	}
	if err := os.Symlink(retargeted, alias); err != nil {
		t.Fatalf("retarget selected ancestor alias: %v", err)
	}
	opened, err := capability.OpenRootDirectory()
	if err != nil {
		t.Fatalf("open retained root directory: %v", err)
	}
	defer opened.Close()
	openedInfo, err := opened.Stat()
	if err != nil {
		t.Fatalf("stat retained root directory: %v", err)
	}
	admittedInfo, err := os.Stat(admitted)
	if err != nil {
		t.Fatalf("stat admitted directory: %v", err)
	}
	if !os.SameFile(openedInfo, admittedInfo) {
		t.Fatalf("retained root no longer identifies the admitted ancestor")
	}
	retargetedInfo, err := os.Stat(retargeted)
	if err != nil {
		t.Fatalf("stat retargeted directory: %v", err)
	}
	if os.SameFile(openedInfo, retargetedInfo) {
		t.Fatalf("retained root followed the retargeted alias")
	}
}

func TestCaptureRootResolvesAliasOnceAndIssuesIndependentCapability(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	alias := filepath.Join(filepath.Dir(root), "project-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("create captured root alias: %v", err)
	}

	direct := mustCaptureRoot(t, root)
	defer direct.Close()
	throughAlias := mustCaptureRoot(t, alias)
	defer throughAlias.Close()
	directAuthority := mustCapturedAuthority(t, direct)
	aliasAuthority := mustCapturedAuthority(t, throughAlias)
	if !directAuthority.Equal(aliasAuthority) {
		t.Fatalf("alias authority %#v does not equal direct authority %#v", aliasAuthority, directAuthority)
	}

	relative, err := NewRelativeDestination(".agents/skills/review")
	if err != nil {
		t.Fatalf("NewRelativeDestination returned error: %v", err)
	}
	destination, err := aliasAuthority.Bind(relative)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	capability, err := throughAlias.Acquire(destination)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if err := throughAlias.Close(); err != nil {
		t.Fatalf("close captured alias root: %v", err)
	}
	if err := capability.Validate(); err != nil {
		t.Fatalf("capability did not retain independent root witness: %v", err)
	}
	rootFile, err := capability.OpenRootDirectory()
	if err != nil {
		t.Fatalf("OpenRootDirectory returned error: %v", err)
	}
	if err := capability.ValidateDirectoryHandle(rootFile.Fd()); err != nil {
		t.Fatalf("ValidateDirectoryHandle(root) returned error: %v", err)
	}
	if err := rootFile.Close(); err != nil {
		t.Fatalf("close duplicate root descriptor: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close capability: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("second capability close returned error: %v", err)
	}
	if !hasFailureKind(capability.Validate(), FailureRootUnavailable) {
		t.Fatalf("closed capability Validate error = %v, want %s", capability.Validate(), FailureRootUnavailable)
	}
}

func TestAuthorityProvenanceMatchesIndependentRecaptureAndRejectsReplacement(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}

	first := mustCaptureRoot(t, root)
	provenance, err := mustCapturedAuthority(t, first).Provenance()
	if err != nil {
		t.Fatalf("Provenance returned error: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first capture: %v", err)
	}

	second := mustCaptureRoot(t, root)
	if err := provenance.Match(mustCapturedAuthority(t, second)); err != nil {
		t.Fatalf("independent recapture did not match persisted provenance: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second capture: %v", err)
	}

	moved := filepath.Join(parent, "moved-project")
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move original root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement root: %v", err)
	}
	replacement := mustCaptureRoot(t, root)
	defer replacement.Close()
	if err := provenance.Match(mustCapturedAuthority(t, replacement)); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replacement match error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestCapturedRootValidatesSelectedAliasStillNamesCapturedAuthority(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	other := filepath.Join(parent, "other")
	for _, directory := range []string{root, other} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create directory %q: %v", directory, err)
		}
	}
	alias := filepath.Join(parent, "selected")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("create selected-root alias: %v", err)
	}
	captured := mustCaptureRoot(t, alias)
	defer captured.Close()
	if err := captured.ValidateSelection(alias); err != nil {
		t.Fatalf("ValidateSelection returned error for unchanged alias: %v", err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatalf("remove selected-root alias: %v", err)
	}
	if err := os.Symlink(other, alias); err != nil {
		t.Fatalf("retarget selected-root alias: %v", err)
	}
	if err := captured.ValidateSelection(alias); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("retargeted selection error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestCaptureRootNoFollowRejectsSelectedAndAncestorSymlinks(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve test root: %v", err)
	}
	physical := filepath.Join(parent, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatalf("create physical root: %v", err)
	}

	selectedLink := filepath.Join(parent, "selected")
	if err := os.Symlink(physical, selectedLink); err != nil {
		t.Fatalf("create selected-root symlink: %v", err)
	}
	if _, err := CaptureRootNoFollow(selectedLink); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("selected-root symlink error = %v, want %s", err, FailureRootReplaced)
	}

	ancestorLink := filepath.Join(parent, "ancestor")
	if err := os.Symlink(parent, ancestorLink); err != nil {
		t.Fatalf("create ancestor symlink: %v", err)
	}
	if _, err := CaptureRootNoFollow(filepath.Join(ancestorLink, "physical")); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("ancestor symlink error = %v, want %s", err, FailureRootReplaced)
	}

	captured, err := CaptureRootNoFollow(physical)
	if err != nil {
		t.Fatalf("CaptureRootNoFollow physical root returned error: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("close no-follow root: %v", err)
	}
}

func TestNoFollowWorkingDirectoryCapabilityRejectsSymlinkRetargetToSameObject(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve test root: %v", err)
	}
	selected := filepath.Join(parent, "selected")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatalf("create selected root: %v", err)
	}
	captured, err := CaptureRootNoFollow(selected)
	if err != nil {
		t.Fatalf("CaptureRootNoFollow returned error: %v", err)
	}
	defer captured.Close()

	capability, err := captured.AcquireSelectedWorkingDirectory(selected)
	if err != nil {
		t.Fatalf("AcquireSelectedWorkingDirectory returned error: %v", err)
	}
	defer capability.Close()

	moved := filepath.Join(parent, "moved")
	if err := os.Rename(selected, moved); err != nil {
		t.Fatalf("move selected root: %v", err)
	}
	if err := os.Symlink(moved, selected); err != nil {
		t.Fatalf("replace selected root with symlink: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("symlink retarget error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestSelectedWorkingDirectoryCapabilityRejectsRetargetedAlias(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	other := filepath.Join(parent, "other")
	for _, directory := range []string{root, other} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create directory %q: %v", directory, err)
		}
	}
	alias := filepath.Join(parent, "selected")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("create selected-root alias: %v", err)
	}
	captured := mustCaptureRoot(t, alias)
	defer captured.Close()
	capability, err := captured.AcquireSelectedWorkingDirectory(alias)
	if err != nil {
		t.Fatalf("AcquireSelectedWorkingDirectory returned error: %v", err)
	}
	defer capability.Close()

	if err := os.Remove(alias); err != nil {
		t.Fatalf("remove selected-root alias: %v", err)
	}
	if err := os.Symlink(other, alias); err != nil {
		t.Fatalf("retarget selected-root alias: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("retargeted capability error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestWorkingDirectoryCapabilityRetainsIndependentRootWitness(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := mustCaptureRoot(t, root)
	capability, err := captured.AcquireWorkingDirectory()
	if err != nil {
		t.Fatalf("AcquireWorkingDirectory returned error: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("close captured root: %v", err)
	}
	directory, err := capability.OpenDirectory()
	if err != nil {
		t.Fatalf("OpenDirectory returned error: %v", err)
	}
	info, err := directory.Stat()
	if err != nil {
		t.Fatalf("stat opened working directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("opened working directory mode = %v, want directory", info.Mode())
	}
	if err := directory.Close(); err != nil {
		t.Fatalf("close opened working directory: %v", err)
	}

	moved := filepath.Join(parent, "moved-project")
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move captured root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement root: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replaced working-directory capability error = %v, want %s", err, FailureRootReplaced)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close working-directory capability: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("second working-directory capability close: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootUnavailable) {
		t.Fatalf("closed working-directory capability error = %v, want %s", err, FailureRootUnavailable)
	}
}

func TestCapturedRootRejectsReplacementAndForeignAuthority(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := mustCaptureRoot(t, root)
	defer captured.Close()
	authority := mustCapturedAuthority(t, captured)
	relative, err := NewRelativeDestination(".agents/skills/review")
	if err != nil {
		t.Fatalf("NewRelativeDestination returned error: %v", err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	capability, err := captured.Acquire(destination)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer capability.Close()

	otherRoot := filepath.Join(parent, "other")
	if err := os.Mkdir(otherRoot, 0o700); err != nil {
		t.Fatalf("create other root: %v", err)
	}
	other := mustCaptureRoot(t, otherRoot)
	defer other.Close()
	otherDestination, err := mustCapturedAuthority(t, other).Bind(relative)
	if err != nil {
		t.Fatalf("bind other destination: %v", err)
	}
	if _, err := captured.Acquire(otherDestination); !hasFailureKind(err, FailureInvalidDestination) {
		t.Fatalf("foreign destination Acquire error = %v, want %s", err, FailureInvalidDestination)
	}

	moved := filepath.Join(parent, "moved-project")
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move captured root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement root: %v", err)
	}
	if err := capability.Validate(); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replaced root capability error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestCommitCapabilityRejectsDescendantMountCrossing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := mustCaptureRoot(t, root)
	defer captured.Close()
	relative, err := NewRelativeDestination(".agents/skills/review")
	if err != nil {
		t.Fatalf("NewRelativeDestination returned error: %v", err)
	}
	destination, err := mustCapturedAuthority(t, captured).Bind(relative)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	capability, err := captured.Acquire(destination)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer capability.Close()

	foreign, err := os.Open("/dev")
	if err != nil {
		t.Skipf("open foreign mount: %v", err)
	}
	defer foreign.Close()
	if err := capability.ValidateDirectoryHandle(foreign.Fd()); !hasFailureKind(err, FailureMountChanged) {
		t.Fatalf("foreign mount validation error = %v, want %s", err, FailureMountChanged)
	}
}

func TestDirectoryMountBoundaryRejectsForeignMount(t *testing.T) {
	root := t.TempDir()
	rootDirectory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rootDirectory.Close()
	boundary, err := CaptureDirectoryMountBoundary(rootDirectory.Fd())
	if err != nil {
		t.Fatalf("CaptureDirectoryMountBoundary: %v", err)
	}

	foreign, err := os.Open("/dev")
	if err != nil {
		t.Skipf("open foreign mount: %v", err)
	}
	defer foreign.Close()
	if err := boundary.ValidateDirectoryHandle(foreign.Fd()); !hasFailureKind(err, FailureMountChanged) {
		t.Fatalf("foreign mount validation error = %v, want %s", err, FailureMountChanged)
	}
}

func TestClosedCapturedRootCannotIssueCapability(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := mustCaptureRoot(t, root)
	authority := mustCapturedAuthority(t, captured)
	relative, err := NewRelativeDestination(".agents/skills/review")
	if err != nil {
		t.Fatalf("NewRelativeDestination returned error: %v", err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
	if _, err := captured.Acquire(destination); !hasFailureKind(err, FailureRootUnavailable) {
		t.Fatalf("closed root Acquire error = %v, want %s", err, FailureRootUnavailable)
	}
}

func mustCaptureRoot(t *testing.T, path string) *CapturedRoot {
	t.Helper()
	root, err := CaptureRoot(path)
	if err != nil {
		t.Fatalf("CaptureRoot(%q) returned error: %v", path, err)
	}
	return root
}

func mustCapturedAuthority(t *testing.T, root *CapturedRoot) Authority {
	t.Helper()
	authority, err := root.Authority()
	if err != nil {
		t.Fatalf("CapturedRoot.Authority returned error: %v", err)
	}
	return authority
}
