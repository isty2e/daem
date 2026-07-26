package authoring

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/target"
)

func TestUnmanageExtensionDryRunBuildsExactPlanWithoutWrites(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	manifest := []byte(unmanageManifest("context7@official", target.ScopeProject))
	writeUnmanageFile(t, paths.ManifestPath, manifest)
	fixture := newUnmanageTestFixture(
		t,
		"context7",
		"context7@official",
		target.ScopeProject,
	)
	owner := unmanageTestOwner(t, paths)
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, fixture.identity, fixture.request)
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
	stateBefore := readUnmanageFile(t, paths.StatefilePath)

	result, err := UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeDryRun,
	})
	if err != nil {
		t.Fatalf("UnmanageExtension returned error: %v", err)
	}
	if result.ManifestStatus != UnmanageManifestStatusWouldRemove ||
		result.ManagementStatus != UnmanageManagementStatusWouldRelease ||
		result.StatefileStatus != UnmanageStateStatusWouldWrite ||
		!result.HostStateRetained {
		t.Fatalf("dry-run result = %#v", result)
	}
	if string(readUnmanageFile(t, paths.ManifestPath)) != string(manifest) ||
		string(readUnmanageFile(t, paths.StatefilePath)) != string(stateBefore) {
		t.Fatal("dry-run changed manifest or state")
	}
	if _, err := os.Stat(paths.LockfilePath); !os.IsNotExist(err) {
		t.Fatalf("dry-run lockfile stat error = %v, want absent", err)
	}
}

func TestUnmanageExtensionCommitsProjectDeclarationAndClaimTogether(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(
		t,
		paths.ManifestPath,
		[]byte(unmanageManifest("context7@official", target.ScopeProject)),
	)
	fixture := newUnmanageTestFixture(
		t,
		"context7",
		"context7@official",
		target.ScopeProject,
	)
	claim := unmanageTestClaim(t, fixture, unmanageTestOwner(t, paths))
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageState(t, paths.StatefilePath, snapshot)

	result, err := UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	})
	if err != nil {
		t.Fatalf("UnmanageExtension returned error: %v", err)
	}
	if result.ManifestStatus != UnmanageManifestStatusRemoved ||
		result.ManagementStatus != UnmanageManagementStatusReleased ||
		result.StatefileStatus != UnmanageStateStatusWritten ||
		result.LockfileStatus != LockfileStatusWritten ||
		!result.HostStateRetained {
		t.Fatalf("write result = %#v", result)
	}
	if strings.Contains(string(readUnmanageFile(t, paths.ManifestPath)), "[[extension]]") {
		t.Fatal("unmanage retained the selected declaration")
	}
	current, err := statefile.Load(context.Background(), paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.ManagedCarrierClaims()) != 0 {
		t.Fatalf("managed claims after unmanage = %#v", current.ManagedCarrierClaims())
	}
}

func TestUnmanageExtensionReleasesGlobalClaimAfterManualOmission(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	manifest := []byte("version = 1\ntargets = [\"claude-code\"]\n")
	writeUnmanageFile(t, paths.ManifestPath, manifest)
	fixture := newUnmanageTestFixture(
		t,
		"context7",
		"context7@official",
		target.ScopeGlobal,
	)
	claim := unmanageTestClaim(t, fixture, unmanageTestOwner(t, paths))
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageRegistry(t, paths.CarrierClaimRegistryPath, registry)

	result, err := UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Scope:        target.ScopeGlobal,
		Mode:         UnmanageModeWrite,
	})
	if err != nil {
		t.Fatalf("UnmanageExtension returned error: %v", err)
	}
	if result.ManifestStatus != UnmanageManifestStatusUnchanged ||
		result.ManagementStatus != UnmanageManagementStatusReleased ||
		result.RegistryStatus != UnmanageStateStatusWritten ||
		!result.AmbientConsumersUnobservable {
		t.Fatalf("manual-omission result = %#v", result)
	}
	if string(readUnmanageFile(t, paths.ManifestPath)) != string(manifest) {
		t.Fatal("manual-omission unmanage changed the manifest")
	}
	current, err := carrierclaim.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := current.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.Claims()) != 0 {
		t.Fatalf("global claims after unmanage = %#v", claims.Claims())
	}
}

func TestUnmanageExtensionReleasesAdoptedClaimWithoutHostMutation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root := t.TempDir()
			configureUnmanageTestHomes(t, root)
			paths := unmanageTestPaths(t, root)
			writeUnmanageFile(
				t,
				paths.ManifestPath,
				[]byte(unmanageManifest("context7@official", scope)),
			)
			fixture := newUnmanageTestFixture(t, "context7", "context7@official", scope)
			claim := unmanageTestClaimWithProvenance(
				t,
				fixture,
				unmanageTestOwner(t, paths),
				durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
			)
			if scope == target.ScopeProject {
				snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
					ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
				})
				if err != nil {
					t.Fatal(err)
				}
				writeUnmanageState(t, paths.StatefilePath, snapshot)
			} else {
				registry, err := durablecarrier.NewGlobalCarrierClaims(
					[]durablecarrier.ManagedCarrierClaim{claim},
				)
				if err != nil {
					t.Fatal(err)
				}
				writeUnmanageRegistry(t, paths.CarrierClaimRegistryPath, registry)
			}

			result, err := UnmanageExtension(context.Background(), UnmanageExtensionRequest{
				ManifestPath: paths.ManifestPath,
				ID:           "context7",
				Scope:        scope,
				Mode:         UnmanageModeWrite,
			})
			if err != nil {
				t.Fatalf("UnmanageExtension: %v", err)
			}
			if result.ManagementStatus != UnmanageManagementStatusReleased ||
				!result.HostStateRetained {
				t.Fatalf("unmanage adopted result = %#v, want released authority and retained host", result)
			}
			if scope == target.ScopeProject {
				state, err := statefile.Load(context.Background(), paths.StatefilePath)
				if err != nil {
					t.Fatal(err)
				}
				if len(state.ManagedCarrierClaims()) != 0 {
					t.Fatalf("project claims after unmanage = %#v", state.ManagedCarrierClaims())
				}
			} else {
				store, err := carrierclaim.New(paths.CarrierClaimRegistryPath)
				if err != nil {
					t.Fatal(err)
				}
				registry, err := store.Load(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if len(registry.Claims()) != 0 {
					t.Fatalf("global claims after unmanage = %#v", registry.Claims())
				}
			}
		})
	}
}

func TestUnmanageExtensionRejectsAlreadyAbsentAndForeignClaim(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(
		t,
		paths.ManifestPath,
		[]byte("version = 1\ntargets = [\"claude-code\"]\n"),
	)
	fixture := newUnmanageTestFixture(
		t,
		"context7",
		"context7@official",
		target.ScopeGlobal,
	)
	foreignPaths := unmanageTestPaths(t, t.TempDir())
	foreignClaim := unmanageTestClaim(t, fixture, unmanageTestOwner(t, foreignPaths))
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{foreignClaim})
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageRegistry(t, paths.CarrierClaimRegistryPath, registry)
	before := readUnmanageFile(t, paths.CarrierClaimRegistryPath)

	_, err = UnmanageExtension(context.Background(), UnmanageExtensionRequest{
		ManifestPath: paths.ManifestPath,
		ID:           "context7",
		Mode:         UnmanageModeWrite,
	})
	if !errors.Is(err, ErrUnmanageExtensionNotFound) {
		t.Fatalf("error = %v, want ErrUnmanageExtensionNotFound", err)
	}
	if string(readUnmanageFile(t, paths.CarrierClaimRegistryPath)) != string(before) {
		t.Fatal("foreign claim changed after rejected unmanage")
	}
}
