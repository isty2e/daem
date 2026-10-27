package cli_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeKeyRecoveryCleanupDoesNotReadHostStateOrRegistry(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newNativeRecoveryFixture(t, kind)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "retirement", "apply", "--yes")
			if code != nativeRecoveryExit || !strings.Contains(stderr, "native recovery confirmed retired journal") {
				t.Fatalf("producer did not lose the process after journal retirement: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			for _, path := range []string{fixture.destination, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath} {
				if err := os.WriteFile(path, []byte("deliberately invalid unrelated content\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotNativeRecoveryPaths(t, fixture.destination, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)
			requireNativeRecoveryDryRun(t, fixture, "retained_cleanup_residue")
			runNativeRecoveryCommand(t, fixture, "none", "recover", "--yes")
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
			if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.destination, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)) {
				t.Fatal("cleanup-only native recovery changed host, state or Registry")
			}
			code, _, _ = invokeNativeRecoveryCommand(t, fixture, "none", "recover", "--yes")
			if code != 1 || !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.destination, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)) {
				t.Fatal("repeated recovery did not report no operation without changing unrelated files")
			}
		})
	}
}

func (store *nativeRecoveryStore) RenameRootedEntry(ctx context.Context, capability rootedpath.CommitCapability, destinationName string, expected mutationfs.EntryIdentity) (mutationfs.CommitOutcome, error) {
	if store.boundary != "retirement" {
		return store.Adapter.RenameRootedEntry(ctx, capability, destinationName, expected)
	}
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	_, journalErr := os.Stat(filepath.Join(path, "journal.json"))
	outcome, err := store.Adapter.RenameRootedEntry(ctx, capability, destinationName, expected)
	if err != nil || journalErr != nil {
		return outcome, err
	}
	retired := filepath.Join(filepath.Dir(path), destinationName)
	if _, err := os.Stat(filepath.Join(retired, "journal.json")); err != nil {
		return outcome, fmt.Errorf("retired journal was not published: %w", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return outcome, fmt.Errorf("active journal was not retired: %v", err)
	}
	fmt.Fprintln(os.Stderr, "native recovery confirmed retired journal")
	os.Exit(nativeRecoveryExit)
	return outcome, nil
}
