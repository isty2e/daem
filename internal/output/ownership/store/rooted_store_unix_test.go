//go:build darwin || linux

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestRootedStoreKeepsRegistryOnCapturedDataRootAfterAliasRetarget(t *testing.T) {
	root := canonicalTestRoot(t)
	dataA := filepath.Join(root, "data-a")
	dataB := filepath.Join(root, "data-b")
	for _, path := range []string{dataA, dataB} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "data")
	if err := os.Symlink(dataA, alias); err != nil {
		t.Skipf("create data-root alias: %v", err)
	}
	selected := filepath.Join(alias, "ownership", "claims.json")
	captured, destination, err := rootedpath.CaptureDestination(selected)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = captured.Close() })
	registryStore, err := NewRooted(captured, destination)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dataB, alias); err != nil {
		t.Fatal(err)
	}

	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "host", "AGENTS.md"), "")
	value, _ := ownership.PresentClaim(claim)
	if _, err := registryStore.Apply(context.Background(), claim.Address(), ownership.NoClaim(), value); err != nil {
		t.Fatalf("rooted Store.Apply returned error: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dataA, "ownership", "claims.json")); err != nil {
		t.Fatalf("captured registry was not written below data A: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dataB, "ownership", "claims.json")); !os.IsNotExist(err) {
		t.Fatalf("retargeted data B received registry effect: %v", err)
	}
	loaded, err := registryStore.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, present := loaded.Exact(claim.Address()); !present || !got.Equal(claim) {
		t.Fatal("rooted registry load lost the claim written below captured data A")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := registryStore.Load(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("rooted Store.Load error = %v, want context cancellation", err)
	}
}
