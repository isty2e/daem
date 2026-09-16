package migrate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestStateMigrationRefusesConflictingOrMissingManagement(t *testing.T) {
	for _, name := range []string{"destination", "reserved", "shared_authority", "foreign_authority", "destination_claim", "missing_snapshot", "no_management", "metadata_overlap", "metadata_ancestor"} {
		t.Run(name, func(t *testing.T) {
			paths, legacy := migrationFixture(t)
			from, err := authorityFor(legacy)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "destination":
				writeFixture(t, paths.StatefilePath, []byte("independent destination"))
			case "missing_snapshot", "no_management":
				if name == "missing_snapshot" {
					seedClaims(t, paths, legacy)
				}
				if err := os.Remove(legacy.StatefilePath); err != nil {
					t.Fatal(err)
				}
			default:
				owner := from
				output := filepath.Join(os.Getenv("HOME"), "output")
				if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
					t.Fatal(err)
				}
				if name == "shared_authority" {
					owner, err = stateauthority.New(from.StatefileAuthority(), filepath.Join(paths.ManifestRoot, "other.toml"))
					if err != nil {
						t.Fatal(err)
					}
				}
				if name == "foreign_authority" {
					foreign := legacy
					foreign.StatefilePath = filepath.Join(t.TempDir(), "state.json")
					owner, err = authorityFor(foreign)
					if err != nil {
						t.Fatal(err)
					}
				}
				if name == "destination_claim" {
					if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
						t.Fatal(err)
					}
					owner, err = authorityFor(paths)
					if err != nil {
						t.Fatal(err)
					}
				}
				if name == "metadata_overlap" {
					output = legacy.StatefilePath
				}
				if name == "metadata_ancestor" {
					output = paths.StateDir
					if err := os.MkdirAll(output, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				claim := outputClaim(t, owner, output)
				if name == "reserved" {
					claim, err = ownership.NewReservedClaim(claim.Address(), owner, "interrupted-operation")
					if err != nil {
						t.Fatal(err)
					}
				}
				registry, err := ownership.NewRegistry([]ownership.Claim{claim})
				if err != nil {
					t.Fatal(err)
				}
				content, err := ownershipstore.Marshal(registry)
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, paths.OwnershipRegistryPath, content)
			}
			before := captureMetadata(t, paths, legacy)
			if _, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath}); err == nil {
				t.Fatal("invalid migration admitted")
			}
			assertMetadata(t, before)
		})
	}
}

func TestStateMigrationRefusesAlreadyIdenticalAuthority(t *testing.T) {
	paths, legacy := migrationFixture(t)
	stateHome := t.TempDir()
	if err := os.Symlink(legacy.StateDir, filepath.Join(stateHome, "daem")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", stateHome)
	before := captureMetadata(t, paths, legacy)
	if _, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath}); err == nil {
		t.Fatal("same authority admitted a transfer")
	}
	assertMetadata(t, before)
}

func TestStateMigrationRefusesStaleDisclosureBeforePublication(t *testing.T) {
	for _, changed := range []string{"source", "output_registry", "carrier_registry", "destination"} {
		t.Run(changed, func(t *testing.T) {
			paths, legacy := migrationFixture(t)
			seedClaims(t, paths, legacy)
			prepared, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			path := map[string]string{"source": legacy.StatefilePath, "output_registry": paths.OwnershipRegistryPath, "carrier_registry": paths.CarrierClaimRegistryPath, "destination": paths.StatefilePath}[changed]
			writeFixture(t, path, []byte("changed after disclosure"))
			before := captureMetadata(t, paths, legacy)
			_, err = prepared.Execute(t.Context())
			var stale mutation.StaleSnapshotError
			if !errors.As(err, &stale) {
				t.Fatalf("error = %v, want stale snapshot", err)
			}
			assertMetadata(t, before)
		})
	}
}

func TestStateMigrationEstablishesNormalizationSensitiveDestination(t *testing.T) {
	paths, _ := migrationFixture(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "\u00e9", "state"))
	prepared, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := daempaths.Resolve(paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := recoverygate.RequireClear(t.Context(), current); err != nil {
		t.Fatal(err)
	}
}

func captureMetadata(t *testing.T, paths, legacy daempaths.Paths) map[string]*metadatatx.Image {
	t.Helper()
	images := make(map[string]*metadatatx.Image)
	for _, path := range []string{paths.StatefilePath, legacy.StatefilePath, paths.OwnershipRegistryPath, paths.CarrierClaimRegistryPath} {
		images[path] = captureImage(t, path)
	}
	return images
}

func assertMetadata(t *testing.T, images map[string]*metadatatx.Image) {
	t.Helper()
	for path, before := range images {
		after := captureImage(t, path)
		if before == nil {
			if after != nil {
				t.Fatalf("created %s", path)
			}
		} else if after == nil || !bytes.Equal(before.Content, after.Content) || before.Mode != after.Mode {
			t.Fatalf("changed %s", path)
		}
	}
}
