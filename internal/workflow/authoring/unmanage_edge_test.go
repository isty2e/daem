package authoring

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/target"
)

func TestUnmanageExtensionRejectsDeclarationClaimIdentityDrift(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	manifest := []byte(unmanageManifest("context7@official", target.ScopeProject))
	writeUnmanageFile(t, paths.ManifestPath, manifest)
	replacement := newUnmanageTestFixture(
		t,
		"context7",
		"context7@other",
		target.ScopeProject,
	)
	claim := unmanageTestClaim(t, replacement, unmanageTestOwner(t, paths))
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageState(t, paths.StatefilePath, snapshot)
	stateBefore := readUnmanageFile(t, paths.StatefilePath)

	_, err = UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("identity drift error = %v", err)
	}
	if string(readUnmanageFile(t, paths.ManifestPath)) != string(manifest) ||
		string(readUnmanageFile(t, paths.StatefilePath)) != string(stateBefore) {
		t.Fatal("identity-drift rejection changed metadata")
	}
	if _, err := os.Stat(paths.LockfilePath); !os.IsNotExist(err) {
		t.Fatalf("identity-drift lockfile stat error = %v, want absent", err)
	}
}

func TestUnmanageExtensionTreatsTargetAndScopeAsSafetyFilters(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	manifest := []byte(unmanageManifest("context7@official", target.ScopeProject))
	writeUnmanageFile(t, paths.ManifestPath, manifest)

	for _, request := range []UnmanageExtensionRequest{
		{
			ManifestPath: paths.ManifestPath,
			ID:           "context7",
			Target:       target.TargetCodex,
			Mode:         UnmanageModeDryRun,
		},
		{
			ManifestPath: paths.ManifestPath,
			ID:           "context7",
			Scope:        target.ScopeGlobal,
			Mode:         UnmanageModeDryRun,
		},
	} {
		_, err := UnmanageExtension(context.Background(), request)
		if !errors.Is(err, ErrUnmanageExtensionNotFound) {
			t.Fatalf("filter request %#v error = %v, want not found", request, err)
		}
	}
	if string(readUnmanageFile(t, paths.ManifestPath)) != string(manifest) {
		t.Fatal("safety-filter rejection changed manifest")
	}
}

func TestUnmanageExtensionFailsClosedOnCorruptState(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	manifest := []byte(unmanageManifest("context7@official", target.ScopeProject))
	writeUnmanageFile(t, paths.ManifestPath, manifest)
	writeUnmanageFile(t, paths.StatefilePath, []byte(`{"version":7,"unknown":true}`))

	_, err := UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	})
	if err == nil || !strings.Contains(err.Error(), "statefile") {
		t.Fatalf("corrupt-state error = %v", err)
	}
	if string(readUnmanageFile(t, paths.ManifestPath)) != string(manifest) {
		t.Fatal("corrupt-state rejection changed manifest")
	}
}

func TestUnmanageExtensionFailsClosedOnCorruptGlobalRegistry(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	manifest := []byte(unmanageManifest("context7@official", target.ScopeGlobal))
	writeUnmanageFile(t, paths.ManifestPath, manifest)
	writeUnmanageFile(
		t,
		paths.CarrierClaimRegistryPath,
		[]byte(`{"version":1,"claims":[],"unknown":true}`),
	)

	_, err := UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	})
	if err == nil || !strings.Contains(err.Error(), "carrier claim registry") {
		t.Fatalf("corrupt-registry error = %v", err)
	}
	if string(readUnmanageFile(t, paths.ManifestPath)) != string(manifest) {
		t.Fatal("corrupt-registry rejection changed manifest")
	}
}

func TestUnmanageExtensionHonorsPreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := UnmanageExtension(ctx, UnmanageExtensionRequest{
		ManifestPath: "unused",
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v, want context.Canceled", err)
	}
}

func TestUnmanageExtensionRejectsMultipleOwnedIdentitiesForPresentDeclaration(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	manifest := []byte(unmanageManifest("context7@official", target.ScopeGlobal))
	writeUnmanageFile(t, paths.ManifestPath, manifest)
	owner := unmanageTestOwner(t, paths)
	expected := newUnmanageTestFixture(
		t,
		"context7",
		"context7@official",
		target.ScopeGlobal,
	)
	drifted := newUnmanageTestFixture(
		t,
		"context7",
		"context7@other",
		target.ScopeGlobal,
	)
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{
		unmanageTestClaim(t, expected, owner),
	})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageRegistry(t, paths.CarrierClaimRegistryPath, registry)
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, drifted.identity, drifted.request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageState(t, paths.StatefilePath, snapshot)
	registryBefore := readUnmanageFile(t, paths.CarrierClaimRegistryPath)
	stateBefore := readUnmanageFile(t, paths.StatefilePath)

	_, err = UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("multiple-identity error = %v, want declaration conflict", err)
	}
	if string(readUnmanageFile(t, paths.ManifestPath)) != string(manifest) ||
		string(readUnmanageFile(t, paths.CarrierClaimRegistryPath)) != string(registryBefore) ||
		string(readUnmanageFile(t, paths.StatefilePath)) != string(stateBefore) {
		t.Fatal("multiple-identity rejection changed metadata")
	}
	if _, err := os.Stat(paths.LockfilePath); !os.IsNotExist(err) {
		t.Fatalf("multiple-identity lockfile stat error = %v, want absent", err)
	}
}

func TestUnmanageExtensionRetiresOnlySelectedOwnerFromSharedGlobalCarrier(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(
		t,
		paths.ManifestPath,
		[]byte(unmanageManifest("context7@official", target.ScopeGlobal)),
	)
	fixture := newUnmanageTestFixture(
		t,
		"context7",
		"context7@official",
		target.ScopeGlobal,
	)
	selectedClaim := unmanageTestClaim(t, fixture, unmanageTestOwner(t, paths))
	foreignPaths := unmanageTestPaths(t, t.TempDir())
	foreignClaim := unmanageTestClaim(t, fixture, unmanageTestOwner(t, foreignPaths))
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{
		selectedClaim,
		foreignClaim,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageRegistry(t, paths.CarrierClaimRegistryPath, registry)

	result, err := UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	})
	if err != nil {
		t.Fatalf("UnmanageExtension returned error: %v", err)
	}
	if result.ManagementStatus != UnmanageManagementStatusReleased ||
		result.RegistryStatus != UnmanageStateStatusWritten {
		t.Fatalf("result = %#v, want exact selected-owner release", result)
	}
	store, err := carrierclaim.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	claims := current.Claims()
	if len(claims) != 1 || !claims[0].ExactEqual(foreignClaim) {
		t.Fatalf("claims = %#v, want only foreign shared claim", claims)
	}
}
