//go:build darwin || linux

package ownership_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/ownershiptest"
)

func TestPendingProtectionObservationIsSourceIndependentOwnerScopedAndDefensive(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	observation := ownershipobserve.ObservePendingProtections(t.Context(), fixture.Paths)
	if !observation.HasPending() || observation.KnownClear() || observation.Cause() != nil {
		t.Fatalf("lost own record: %+v", observation)
	}
	rows := observation.Records()
	if len(rows) != 1 || !rows[0].Equal(fixture.Protection) {
		t.Fatal("wrong owner snapshot")
	}
	rows[0] = outputownership.PendingProtection{}
	if !observation.Records()[0].Equal(fixture.Protection) {
		t.Fatal("observation aliases returned rows")
	}
	if destinations := fixture.Protection.Documents()[0].DestinationsForOwner(fixture.Owner.Key()); len(destinations) != 1 || destinations[0].String() != ".codex/config.toml" {
		t.Fatalf("lost owner destinations: %+v", destinations)
	}

	foreign, err := daempaths.Resolve(filepath.Join(t.TempDir(), "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err = foreign.WithDataDir(fixture.Paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	other := ownershipobserve.ObservePendingProtections(t.Context(), foreign)
	if !other.KnownClear() || other.HasPending() {
		t.Fatalf("foreign owner leaked: %+v", other)
	}
}

func TestPendingProtectionObservationDoesNotInventClearInventory(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	canceled := ownershipobserve.ObservePendingProtections(ctx, fixture.Paths)
	if canceled.KnownClear() || !errors.Is(canceled.Cause(), context.Canceled) {
		t.Fatalf("canceled observation: %+v", canceled)
	}
	if err := os.WriteFile(fixture.Paths.OwnershipRegistryPath, []byte("malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	unknown := ownershipobserve.ObservePendingProtections(t.Context(), fixture.Paths)
	if unknown.State() != ownershipobserve.ProtectionUnavailable || unknown.KnownClear() || unknown.Cause() == nil {
		t.Fatalf("invented clear inventory: %+v", unknown)
	}
	if (ownershipobserve.ProtectionObservation{}).KnownClear() || ownershipobserve.UnsupportedProtections().KnownClear() {
		t.Fatal("unobserved or unsupported inventory certified clear")
	}
}

func TestMissingProtectionInventoryDoesNotCreateState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	paths, err := daempaths.Resolve(filepath.Join(root, "project", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	observation := ownershipobserve.ObservePendingProtections(t.Context(), paths)
	if !observation.KnownClear() {
		t.Fatalf("missing inventory not known clear: %+v", observation)
	}
	if _, err := os.Stat(paths.DataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discovery created state: %v", err)
	}
}
