//go:build darwin || linux

package apply

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierWritersSupplySelectedOwnerReferences(t *testing.T) {
	for _, test := range []struct {
		name      string
		absent    bool
		promotion bool
	}{
		{name: "adoption without State", absent: true},
		{name: "adoption with State alias"},
		{name: "pending promotion with State alias", promotion: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			for key, directory := range map[string]string{
				"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state",
			} {
				t.Setenv(key, filepath.Join(base, directory))
			}
			root, manifest, lockfile, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeGlobal)
			selectedRoot := filepath.Join(base, "selected-workspace")
			if err := os.Symlink(root, selectedRoot); err != nil {
				t.Fatal(err)
			}
			manifest = filepath.Join(selectedRoot, "daem.toml")
			lockfile = filepath.Join(selectedRoot, "daem.lock.toml")
			paths := applyTestPaths(t, selectedRoot)
			observed, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			reference := observed.Reference()
			if !test.absent {
				reference = seedWorkflowStateImage(t, paths)
			}
			identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(locked.Locked.Subjects()[0])
			if err != nil || !admitted {
				t.Fatal("carrier fixture has no exact identity", err)
			}
			request, err := lock.DelegatedOperationRequest(locked.Locked.Subjects()[0], lock.OperationInstall)
			if err != nil {
				t.Fatal(err)
			}
			foreign := seedForeignCarrierReference(t, paths, identity, request)
			present := exactClaudeCarrierObservations(t, locked, subject, target.ScopeGlobal)
			if test.promotion {
				owner, err := stateauthority.New(reference.Identity(), manifest)
				if err != nil {
					t.Fatal(err)
				}
				pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
				if err != nil {
					t.Fatal(err)
				}
				before, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 21, PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending}})
				if err != nil {
					t.Fatal(err)
				}
				image, err := statefile.NewStateImage(before, reference)
				if err != nil {
					t.Fatal(err)
				}
				content, err := statefile.MarshalImage(image)
				if err != nil {
					t.Fatal(err)
				}
				writeApplyFile(t, paths.StatefilePath, string(content))
				present = applyClaudeObservationBatch(t, locked, subject, applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
					Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh,
					Rows: []observeclaudeplugin.Row{applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser)},
				}))
			}
			input := CommandInput{ManifestPath: manifest, LockfilePath: lockfile, TargetValues: []string{"claude-code"}, RelationObservations: &present, ManageUnmanagedMatches: !test.promotion}
			host := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				t.Fatal("metadata-only carrier settlement invoked a host command")
				return subprocess.CommandResult{}
			}})
			for attempt := range 2 {
				prepared, err := PlanWrite(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{RelationObservations: &present, HostRouteExecutor: host})
				_ = prepared.Close()
				if err != nil || len(result.HostRouteAttempts) != 0 {
					t.Fatal("carrier settlement failed", err)
				}
				content, err := os.ReadFile(paths.CarrierClaimRegistryPath)
				if err != nil {
					t.Fatal(err)
				}
				image, err := carrierclaimstore.UnmarshalImage(content)
				if err != nil || len(image.Registry().Claims()) != 2 {
					t.Fatal("carrier update downgraded or lost an owner", err)
				}
				for _, expected := range []pathauthority.Reference{reference, foreign} {
					actual, present := image.Reference(expected.Identity())
					if !present || !actual.Equal(expected) {
						t.Fatal("carrier update lost selected or foreign lookup spelling")
					}
				}
				if attempt == 1 && result.ActionCount != 0 {
					t.Fatal("settled carrier retry was not a no-op")
				}
				if test.absent {
					if _, err := os.Lstat(paths.StatefilePath); !errors.Is(err, fs.ErrNotExist) {
						t.Fatal("registry-only adoption created a State file", err)
					}
				} else {
					stateBytes, err := os.ReadFile(paths.StatefilePath)
					if err != nil {
						t.Fatal(err)
					}
					state, err := statefile.UnmarshalImage(stateBytes)
					if err != nil || !state.Reference().Equal(reference) || len(state.Snapshot().PendingCarrierInstalls()) != 0 || state.Snapshot().NativeDocumentCommit() != 21 {
						t.Fatal("carrier settlement changed State format, reference or unrelated facts", err)
					}
				}
			}
		})
	}
}

func seedForeignCarrierReference(t *testing.T, paths daempaths.Paths, identity durablecarrier.ManagedCarrierIdentity, request realizationdelegate.Request) pathauthority.Reference {
	t.Helper()
	foreignRoot := filepath.Join(paths.ManifestRoot, "foreign")
	stateDir := filepath.Join(foreignRoot, ".daem")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(foreignRoot, "lookup")
	if err := os.Symlink(stateDir, alias); err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	reference, err := pathauthority.NewReference(observed.Exact(), filepath.Join(alias, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(observed.Exact(), filepath.Join(foreignRoot, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(owner, identity, request, durablecarrier.ClaimProvenanceInstalledObserved)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	image, err := carrierclaimstore.NewRegistryImage(registry, []pathauthority.Reference{reference})
	if err != nil {
		t.Fatal(err)
	}
	content, err := carrierclaimstore.MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	writeApplyFile(t, paths.CarrierClaimRegistryPath, string(content))
	if reference.Locator() == reference.Identity().Key() {
		t.Fatal("foreign fixture did not retain a distinct alias")
	}
	return reference
}
