//go:build darwin || linux

package refresh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/metadataconversion"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	lockworkflow "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/test/ownershiptest"
)

func TestRefreshUsesFreshPhysicalPlanAfterMetadataConversion(t *testing.T) {
	for _, test := range []struct {
		name    string
		missing bool
		located bool
		foreign bool
	}{
		{name: "legacy State"},
		{name: "already located", located: true},
		{name: "missing State", missing: true},
		{name: "foreign protection defers conversion", foreign: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			configureMetadataRefreshHomes(t, root)
			manifest := writeNoObserverRefreshFixture(t)
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var before []byte
			if !test.missing {
				if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
					t.Fatal(err)
				}
				snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 17})
				if err != nil {
					t.Fatal(err)
				}
				if test.located {
					observed, observeErr := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
					if observeErr != nil {
						t.Fatal(observeErr)
					}
					image, imageErr := statefile.NewStateImage(snapshot, observed.Reference())
					if imageErr != nil {
						t.Fatal(imageErr)
					}
					before, err = statefile.MarshalImage(image)
				} else {
					before, err = statefile.Marshal(snapshot)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.StatefilePath, before, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var registryBefore []byte
			if test.foreign {
				ownershiptest.WritePendingDocumentProtection(t, manifest, filepath.Join(paths.ManifestRoot, "unrelated.json"))
				registryBefore = readTestFile(t, paths.OwnershipRegistryPath)
			}
			prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, ExtensionID: "formatter"}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if !test.missing && !bytes.Equal(readTestFile(t, paths.StatefilePath), before) {
				t.Fatal("read-only planning converted State")
			}
			if _, err := os.Stat(paths.GlobalMetadataDir()); !os.IsNotExist(err) {
				t.Fatal("read-only planning created conversion evidence", err)
			}
			calls := 0
			result, err := Execute(t.Context(), prepared, ExecuteOptions{CommandOptions: subprocess.CommandOptions{
				Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					calls++
					return subprocess.CommandResult{Started: true, HasExitCode: true}
				},
			}})
			if err != nil || calls != 1 || !result.AttemptHistory.Persisted {
				t.Fatal("refresh did not execute the confirmed operation once", err, result)
			}
			current, err := statefile.Load(t.Context(), paths.StatefilePath)
			if err != nil || len(current.HostRouteAttempts()) != 1 || (!test.missing && current.NativeDocumentCommit() != 17) {
				t.Fatal("refresh did not preserve State facts and attempt history", err)
			}
			var version struct{ Version int }
			if err := json.Unmarshal(readTestFile(t, paths.StatefilePath), &version); err != nil {
				t.Fatal(err)
			}
			want := 11
			if test.foreign {
				want = 10
			}
			if version.Version != want {
				t.Fatalf("refresh State version=%d, want %d", version.Version, want)
			}
			if test.foreign && !bytes.Equal(readTestFile(t, paths.OwnershipRegistryPath), registryBefore) {
				t.Fatal("deferred conversion changed foreign protection")
			}
		})
	}
}

func TestRefreshMetadataReplanRetainsApprovedInputs(t *testing.T) {
	for _, change := range []string{"declaration", "logical State", "project root"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			configureMetadataRefreshHomes(t, root)
			manifest := writeNoObserverRefreshFixture(t)
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 17})
			if err != nil {
				t.Fatal(err)
			}
			content, err := statefile.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.StatefilePath, content, 0o600); err != nil {
				t.Fatal(err)
			}
			prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, ExtensionID: "formatter"}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			execution, err := prepared.beginExecution()
			if err != nil {
				t.Fatal(err)
			}
			defer execution.root.Close()
			if err := metadataconversion.ConvertIfNeeded(t.Context(), execution.planned.barrier); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "declaration":
				content := append(readTestFile(t, manifest), []byte("\n# concurrent edit\n")...)
				if err := os.WriteFile(manifest, content, 0o600); err != nil {
					t.Fatal(err)
				}
			case "logical State":
				physical, err := statefile.LoadPhysical(t.Context(), paths.StatefilePath)
				if err != nil {
					t.Fatal(err)
				}
				after, err := physical.Snapshot().AdvanceNativeDocumentCommit()
				if err != nil {
					t.Fatal(err)
				}
				content, err := physical.Codec().EncodeState(after)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.StatefilePath, content, 0o600); err != nil {
					t.Fatal(err)
				}
			case "project root":
				moved := paths.ManifestRoot + "-moved"
				if err := os.Rename(paths.ManifestRoot, moved); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(moved) })
			}
			var stale mutation.StalePlanError
			if err := replanAfterMetadataConversion(t.Context(), &execution); !errors.As(err, &stale) {
				t.Fatal("metadata replan accepted changed approved inputs", err)
			}
		})
	}
}

func TestRefreshMetadataReplanWithManagedNativeSettings(t *testing.T) {
	root := t.TempDir()
	configureMetadataRefreshHomes(t, root)
	manifest := writeNoObserverRefreshFixture(t)
	declaration := readTestFile(t, manifest)
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(paths.ManifestRoot, "preferences.toml")
	if err := os.WriteFile(source, []byte("[tui]\nnotifications=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	native := fmt.Sprintf("\n[[host_config]]\nid='preferences'\ntargets=['codex']\nscope='project'\npath='config.toml'\nsource=%q\nownership='keys'\n", source)
	if err := os.WriteFile(manifest, []byte("version=1\ntargets=['codex']\n"+native), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lockworkflow.RunLock(t.Context(), lockworkflow.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}
	apply, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: manifest})
	if err != nil {
		t.Fatal(err)
	}
	defer apply.Close()
	if _, err := applyworkflow.ExecuteWithOptions(t.Context(), apply, applyworkflow.ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.OwnershipRegistryPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append(declaration, []byte(native)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lockworkflow.RunLock(t.Context(), lockworkflow.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(paths.ManifestRoot, ".codex", "config.toml")
	before := readTestFile(t, host)
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, ExtensionID: "formatter"}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	execution, err := prepared.beginExecution()
	if err != nil {
		t.Fatal(err)
	}
	defer execution.root.Close()
	if err := metadataconversion.ConvertIfNeeded(t.Context(), execution.planned.barrier); err != nil {
		t.Fatal(err)
	}
	if _, err := ownershipstore.UnmarshalImage(readTestFile(t, paths.OwnershipRegistryPath)); err != nil {
		t.Fatal("native ownership registry was not converted", err)
	}
	if err := replanAfterMetadataConversion(t.Context(), &execution); err != nil {
		t.Fatal("native-qualified refresh did not survive metadata conversion", err)
	}
	encoded, err := execution.planned.physicalStateCodec.EncodeState(execution.planned.currentState)
	if err != nil {
		t.Fatal(err)
	}
	image, err := statefile.UnmarshalImage(encoded)
	if err != nil || len(image.Snapshot().ManagedAggregates()) != 1 || len(image.Snapshot().HostRouteAttempts()) != 0 {
		t.Fatal("refresh replan did not retain its native physical epoch", err)
	}
	if !bytes.Equal(before, readTestFile(t, host)) {
		t.Fatal("refresh changed unrelated managed native settings")
	}
}

func TestRefreshMetadataReplanRebindsGlobalPinRegistry(t *testing.T) {
	root := t.TempDir()
	configureMetadataRefreshHomes(t, root)
	manifest := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifest, []byte("version=1\ntargets=['pi']\n[[extension]]\nid='formatter'\ncarrier='pi-package'\nscope='global'\nsource={host_source='git:github.com/example/formatter@main'}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lockworkflow.RunLock(t.Context(), lockworkflow.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{paths.StateDir, filepath.Dir(paths.CarrierClaimRegistryPath)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	content, err := statefile.Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StatefilePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	claims, err := durablecarrier.NewGlobalCarrierClaims(nil)
	if err != nil {
		t.Fatal(err)
	}
	content, err = carrierclaim.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.CarrierClaimRegistryPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, ExtensionID: "formatter"}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if !prepared.lifecycle.planned.pinClaimRegistry {
		t.Fatal("Pi Git fixture did not select global pin-registry evidence")
	}
	result, err := Execute(t.Context(), prepared, ExecuteOptions{CommandOptions: subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{Started: true, HasExitCode: true}
		},
	}})
	if err != nil || !result.AttemptHistory.Persisted {
		t.Fatal("Pi refresh did not rebind converted metadata", err)
	}
	if _, err := carrierclaim.UnmarshalImage(readTestFile(t, paths.CarrierClaimRegistryPath)); err != nil {
		t.Fatal("Pi registry was not converted", err)
	}
}

func TestRefreshMetadataReplanRetainsHostObservation(t *testing.T) {
	root := t.TempDir()
	configureMetadataRefreshHomes(t, root)
	manifest, inventory := writeObservedRefreshFixture(t)
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content, err := statefile.Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StatefilePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	observer := func(ctx context.Context, input ObservationRequest) (RelationObservation, error) {
		return syntheticObserver(t, inventory, observerelation.StateExactCorrelation)(ctx, input)
	}
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, ExtensionID: "context7"}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t), Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	execution, err := prepared.beginExecution()
	if err != nil {
		t.Fatal(err)
	}
	defer execution.root.Close()
	if err := metadataconversion.ConvertIfNeeded(t.Context(), execution.planned.barrier); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inventory, []byte("changed observation"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stale mutation.StalePlanError
	if err := replanAfterMetadataConversion(t.Context(), &execution); !errors.As(err, &stale) {
		t.Fatal("metadata replan lost a host observation", err)
	}
}

func configureMetadataRefreshHomes(t *testing.T, root string) {
	t.Helper()
	for key, directory := range map[string]string{
		"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache",
	} {
		t.Setenv(key, filepath.Join(root, directory))
	}
}
