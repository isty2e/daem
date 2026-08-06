//go:build darwin || linux

package execute

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestCommitFileDestinationAgainstRejectsProjectFinalSymlink(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "config")
	writeMutationTestFile(t, outsideFile, "outside")
	if err := os.Mkdir(filepath.Join(root, ".agent"), 0o700); err != nil {
		t.Fatalf("create project parent: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, ".agent", "config")); err != nil {
		t.Fatalf("create final symlink: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")

	err := commitFileDestinationAgainst(
		context.Background(), authority, destination, []byte("managed"), 0o600, false, nil,
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureFinalSymlink) {
		t.Fatalf("commitFileDestinationAgainst error = %v, want %s", err, rootedpath.FailureFinalSymlink)
	}
	assertMutationTestFile(t, outsideFile, "outside")
}

func TestCommitFileDestinationAgainstRejectsAncestorRedirectAfterCapture(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	originalParent := filepath.Join(root, ".agent")
	if err := os.Mkdir(originalParent, 0o700); err != nil {
		t.Fatalf("create project parent: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	outside := t.TempDir()
	if err := os.Rename(originalParent, originalParent+"-moved"); err != nil {
		t.Fatalf("move project parent: %v", err)
	}
	if err := os.Symlink(outside, originalParent); err != nil {
		t.Fatalf("redirect project parent: %v", err)
	}

	err := commitFileDestinationAgainst(
		context.Background(), authority, destination, []byte("managed"), 0o600, false, nil,
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureAncestorSymlink) {
		t.Fatalf("commitFileDestinationAgainst error = %v, want %s", err, rootedpath.FailureAncestorSymlink)
	}
	if _, err := os.Lstat(filepath.Join(outside, "config")); !os.IsNotExist(err) {
		t.Fatalf("redirected outside destination exists: %v", err)
	}
}

func TestMutateFileDestinationRejectsAncestorSwapBetweenReadAndCommit(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create project parent: %v", err)
	}
	writeMutationTestFile(t, filepath.Join(parent, "config"), "inside")
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	outside := t.TempDir()
	writeMutationTestFile(t, filepath.Join(outside, "config"), "outside")

	outcome := mutateFileDestinationWithOutcome(context.Background(), authority, destination, 0o600, false, 1024, func(existing []byte, _ os.FileMode, exists bool) ([]byte, bool, error) {
		if !exists {
			t.Fatal("mutation input unexpectedly reported missing destination")
		}
		if string(existing) != "inside" {
			t.Fatalf("mutation input = %q", existing)
		}
		if err := os.Rename(parent, parent+"-moved"); err != nil {
			t.Fatalf("move project parent: %v", err)
		}
		if err := os.Symlink(outside, parent); err != nil {
			t.Fatalf("redirect project parent: %v", err)
		}
		return []byte("managed"), true, nil
	})
	err := outcome.err
	if !hasRootedPathFailureKind(err, rootedpath.FailureAncestorSymlink) {
		t.Fatalf("mutateFileDestinationWithOutcome error = %v, want %s", err, rootedpath.FailureAncestorSymlink)
	}
	assertMutationTestFile(t, filepath.Join(outside, "config"), "outside")
	assertMutationTestFile(t, filepath.Join(parent+"-moved", "config"), "inside")
}

func TestAggregateRecoveryRejectsNewlyAppearedEmptyDocument(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	if err := os.Mkdir(filepath.Join(root, ".codex"), 0o700); err != nil {
		t.Fatalf("create aggregate parent: %v", err)
	}
	destinationPath := filepath.Join(root, ".codex", "hooks.json")
	if err := os.WriteFile(destinationPath, nil, 0o600); err != nil {
		t.Fatalf("create unmanaged empty aggregate: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".codex/hooks.json")
	placement, ok := aggregate.HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project Hook placement is missing")
	}
	canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: "Stop", Type: "command", Command: "echo guard",
	})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		t.Fatal(err)
	}
	contract := contribution.Contract()

	_, _, err = restoreAggregateProjection(t.Context(), authority, destination, recoveryHostAction{
		Scope: target.ScopeProject, Destination: ".codex/hooks.json", ContentPath: aggregate.HooksContentPath,
		AggregateContract: &contract,
	}, nil, false, nil, testAggregateCodecs())
	if err == nil || !strings.Contains(err.Error(), "document presence changed") {
		t.Fatalf("restoreAggregateProjection error = %v, want appeared-document rejection", err)
	}
	content, readErr := os.ReadFile(destinationPath)
	if readErr != nil || len(content) != 0 {
		t.Fatalf("unmanaged empty aggregate = %q, %v; want preserved empty file", content, readErr)
	}
}

func TestCommitDirectoryDestinationPublishesRootedTree(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o750); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	writeMutationTestFile(t, filepath.Join(source, "nested", "SKILL.md"), "skill")
	authority, destination := projectMutationDestinationForTest(t, root, ".agents/skills/review")

	precondition, err := captureManagedPathPrecondition(
		context.Background(), authority, destination, false, "", realization.PathProjectionDirectory, nil,
	)
	if err != nil {
		t.Fatalf("capture managed directory precondition: %v", err)
	}
	t.Cleanup(func() { _ = precondition.close() })
	identity, view := directoryPayloadForTest(t, source)
	if err := commitManagedDirectoryDestination(context.Background(), authority, identity, view, destination, &precondition); err != nil {
		t.Fatalf("commitManagedDirectoryDestination returned error: %v", err)
	}
	assertMutationTestFile(t, filepath.Join(root, ".agents", "skills", "review", "nested", "SKILL.md"), "skill")
}

func TestRemoveDestinationAgainstRejectsFinalSymlinkSubstitution(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	outsideFile := filepath.Join(t.TempDir(), "outside")
	writeMutationTestFile(t, outsideFile, "outside")
	destinationPath := filepath.Join(root, ".agent", "config")
	writeMutationTestFile(t, destinationPath, "inside")
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	capability, err := authority.acquire(destination)
	if err != nil {
		t.Fatalf("acquire original destination: %v", err)
	}
	expected, err := authority.filesystem.CaptureRootedEntryIdentity(context.Background(), capability)
	if err != nil {
		t.Fatalf("capture original destination: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close original destination capability: %v", err)
	}
	bindTestFileRemovalIntent(t, authority, destination, []byte("inside"))
	if err := os.Remove(destinationPath); err != nil {
		t.Fatalf("remove original destination: %v", err)
	}
	if err := os.Symlink(outsideFile, destinationPath); err != nil {
		t.Fatalf("substitute final symlink: %v", err)
	}

	err = removeDestinationAgainst(context.Background(), authority, destination, expected)
	if err == nil || !strings.Contains(err.Error(), "rooted removal commit outcome uncommitted") {
		t.Fatalf("removeDestinationAgainst error = %v, want uncommitted identity-change rejection", err)
	}
	assertMutationTestFile(t, outsideFile, "outside")
}

func TestMutationHelpersRejectInvalidDestination(t *testing.T) {
	err := commitFileDestinationAgainst(
		context.Background(), nil, mutationDestination{}, []byte("content"), 0o600, false, nil,
	)
	if err == nil {
		t.Fatal("commitFileDestinationAgainst accepted an invalid destination")
	}
}

func projectMutationDestinationForTest(
	t *testing.T,
	root string,
	relative string,
) (*mutationAuthority, mutationDestination) {
	t.Helper()
	effect := ManagedPathEffect{replace: &managedPathReplaceEffect{facts: managedPathEffectFacts{
		scope: target.ScopeProject, destination: outputtest.Parse(t, relative),
	}}}
	paths := Paths{ManifestRoot: root}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		[]ManagedPathEffect{effect},
		nil,
		nil,
		destinationResolver(paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("newMutationAuthorityWithProjectionEffects returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := authority.close(); err != nil {
			t.Errorf("close mutation authority: %v", err)
		}
	})
	destination, err := authority.resolveBoundDestination(target.ScopeProject, outputtest.Parse(t, relative))
	if err != nil {
		t.Fatalf("resolve mutation destination: %v", err)
	}
	return authority, destination
}

func newProjectRootForMutationTest(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	return root
}

func writeMutationTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create parent for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func assertMutationTestFile(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content at %q = %q, want %q", path, content, want)
	}
}

func directoryPayloadForTest(t *testing.T, source string) (artifact.ExactIdentity, access.View) {
	t.Helper()
	view, err := access.OpenView(source)
	if err != nil {
		t.Fatalf("open directory payload: %v", err)
	}
	hash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatalf("hash directory payload: %v", err)
	}
	identity, err := artifact.NewExactIdentity("test:directory", "", view.Kind(), hash)
	if err != nil {
		t.Fatalf("construct directory payload identity: %v", err)
	}
	return identity, view
}
