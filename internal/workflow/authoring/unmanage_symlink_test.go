package authoring

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/target"
)

func TestUnmanagePreservesLinkedDeclarationPair(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	content := []byte(unmanageManifest("context7@official", target.ScopeProject))
	writeUnmanageFile(t, paths.ManifestPath, content)
	locked, err := BuildLockfileChange(t.Context(), LockfileChangeInput{ManifestPath: paths.ManifestPath, ManifestBytes: content})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageFile(t, paths.LockfilePath, locked.Content())
	fixture := newUnmanageTestFixture(t, "context7", "context7@official", target.ScopeProject)
	claim := unmanageTestClaim(t, fixture, unmanageTestOwner(t, paths))
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim}})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageState(t, paths.StatefilePath, snapshot)
	dot := filepath.Join(root, "dot")
	if err := os.Mkdir(dot, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.ManifestPath, paths.LockfilePath} {
		actual := filepath.Join(dot, filepath.Base(path))
		if err := os.Rename(path, actual); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(actual, path); err != nil {
			t.Fatal(err)
		}
	}
	result, err := UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath, ID: "context7", Mode: UnmanageModeWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ManifestStatus != UnmanageManifestStatusRemoved || result.ManagementStatus != UnmanageManagementStatusReleased || !result.HostStateRetained {
		t.Fatalf("unmanage result = %#v", result)
	}
	current, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil || len(current.ManagedCarrierClaims()) != 0 {
		t.Fatalf("management authority was not released: %v", err)
	}
	for _, path := range []string{paths.ManifestPath, paths.LockfilePath} {
		link, err := os.Readlink(path)
		if err != nil || link != filepath.Join(dot, filepath.Base(path)) {
			t.Fatalf("selected declaration link changed: %q, %v", link, err)
		}
	}
}
