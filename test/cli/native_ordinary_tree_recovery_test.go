package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/test/testkit"
)

func nativeOrdinaryTreeRecoveryFixture(t *testing.T) (nativeRecoveryFixture, string) {
	t.Helper()
	fixture, _ := nativeOrdinaryRecoveryFixture(t)
	workspace := filepath.Dir(fixture.manifest)
	manifest := string(testkit.ReadFile(t, fixture.manifest)) + "\n[[skill]]\nname='sample'\nsource={path='skills/sample',mode='vendor'}\n"
	testkit.WriteFile(t, workspace, filepath.Base(fixture.manifest), manifest)
	testkit.WriteFile(t, workspace, "skills/sample/SKILL.md", "---\nname: sample\ndescription: before\n---\n")
	testkit.WriteFile(t, workspace, "skills/sample/references/detail.txt", "before\n")
	runNativeRecoveryCommand(t, fixture, "none", "lock")
	runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
	testkit.WriteFile(t, workspace, "skills/sample/SKILL.md", "---\nname: sample\ndescription: after\n---\n")
	testkit.WriteFile(t, workspace, "skills/sample/references/detail.txt", "after\n")
	testkit.WriteFile(t, workspace, "preferences.toml", "model='skill-after'\n")
	runNativeRecoveryCommand(t, fixture, "none", "lock")
	return fixture, filepath.Join(workspace, ".agents", "skills", "sample")
}

func TestNativeOrdinaryTreeRecovery(t *testing.T) {
	for _, boundary := range []string{"host", "skill-removal", "return-error", "cancel", "state", "retirement"} {
		t.Run(boundary, func(t *testing.T) {
			fixture, skill := nativeOrdinaryTreeRecoveryFixture(t)
			before := snapshotNativeRecoveryPaths(t, fixture.destination, skill, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)
			watched := fixture
			watched.destination = skill
			code, stdout, stderr := invokeNativeRecoveryCommand(t, watched, boundary, "apply", "--yes", "--verbose")
			if code == 0 || boundary != "return-error" && boundary != "cancel" && code != nativeRecoveryExit {
				t.Fatalf("real tree effect did not reach %s: %d %s %s", boundary, code, stdout, stderr)
			}
			if boundary == "return-error" || boundary == "cancel" {
				if !bytes.Contains([]byte(stderr), []byte(nativeRecoveryReceipt(skill))) {
					t.Fatal("fault preceded actual tree publication", stderr)
				}
			} else {
				classification := "needs_rollback"
				if boundary == "state" {
					classification = "clean_after"
				}
				if boundary == "retirement" {
					classification = "retained_cleanup_residue"
				}
				requireNativeRecoveryDryRun(t, fixture, classification)
				runNativeRecoveryCommand(t, fixture, "none", "recover", "--yes")
			}
			if boundary != "state" && boundary != "retirement" && !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.destination, skill, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)) {
				t.Fatal("joint recovery did not restore the full tree and native Before phase", stderr)
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
			runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
			if testkit.HashDirectory(t, filepath.Join(filepath.Dir(fixture.manifest), "skills", "sample")) != testkit.HashDirectory(t, skill) {
				t.Fatal("retry omitted part of the skill tree")
			}
			runNativeRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
		})
	}
}

func (store *nativeRecoveryStore) PrepareRootedTree(ctx context.Context, capability rootedpath.CommitCapability, populate func(mutationfs.RootedTreeWriter) error) (mutationfs.PreparedRootedTree, error) {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return nil, err
	}
	prepared, err := store.Adapter.PrepareRootedTree(ctx, capability, populate)
	if err != nil || path != store.host {
		return prepared, err
	}
	return nativeRecoveryPreparedTree{PreparedRootedTree: prepared, store: store, path: path}, nil
}

type nativeRecoveryPreparedTree struct {
	mutationfs.PreparedRootedTree
	store *nativeRecoveryStore
	path  string
}

func (prepared nativeRecoveryPreparedTree) Commit(ctx context.Context) error {
	if err := prepared.PreparedRootedTree.Commit(ctx); err != nil {
		return err
	}
	return prepared.afterPublication()
}

func (prepared nativeRecoveryPreparedTree) CommitWithOutcome(ctx context.Context) (mutationfs.CommitOutcome, error) {
	outcome, err := prepared.PreparedRootedTree.CommitWithOutcome(ctx)
	if err != nil {
		return outcome, err
	}
	return outcome, prepared.afterPublication()
}

func (prepared nativeRecoveryPreparedTree) afterPublication() error {
	if info, err := os.Stat(prepared.path); err != nil || !info.IsDir() {
		return fmt.Errorf("tree publication was not confirmed: %v", err)
	}
	fmt.Fprintln(os.Stderr, nativeRecoveryReceipt(prepared.path))
	store := prepared.store
	if store.boundary == "host" {
		os.Exit(nativeRecoveryExit)
	}
	if !store.failed && store.boundary == "return-error" {
		store.failed = true
		return errors.New("native recovery fixture: returned tree publication error")
	}
	if !store.failed && store.boundary == "cancel" {
		store.failed = true
		store.cancel()
		return context.Canceled
	}
	return nil
}

func (store *nativeRecoveryStore) RemoveRootedEntryWithResidue(ctx context.Context, capability rootedpath.CommitCapability, expected mutationfs.EntryIdentity, names mutationfs.LogicalRemovalNames, limits mutationfs.TreeTraversalLimits) (mutationfs.CommitOutcome, error) {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	outcome, err := store.Adapter.RemoveRootedEntryWithResidue(ctx, capability, expected, names, limits)
	if err == nil && path == store.host && store.boundary == "skill-removal" {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return outcome, fmt.Errorf("ordinary retirement was not confirmed: %v", err)
		}
		fmt.Fprintln(os.Stderr, "native recovery confirmed ordinary retirement")
		os.Exit(nativeRecoveryExit)
	}
	return outcome, err
}
