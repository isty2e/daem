package apply

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

func TestExecuteRetiresAlreadyAbsentCarrierClaim(t *testing.T) {
	for _, provenance := range []durablecarrier.ClaimProvenance{
		durablecarrier.ClaimProvenanceInstalledObserved,
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	} {
		t.Run(string(provenance), func(t *testing.T) {
			for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
				t.Run(string(scope), func(t *testing.T) {
					root, manifestPath, lockfilePath, missing, previous, _ := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
					writeApplyLockfile(t, lockfilePath, lock.File{Version: lock.CurrentVersion})
					claim := seedApplyCarrierClaimWithProvenance(
						t,
						root,
						manifestPath,
						previous,
						scope,
						provenance,
					)

					prepared, err := PlanWrite(context.Background(), CommandInput{
						ManifestPath:         manifestPath,
						LockfilePath:         lockfilePath,
						TargetValues:         []string{"claude-code"},
						RelationObservations: &missing,
					})
					if err != nil {
						t.Fatalf("PlanWrite: %v", err)
					}
					absences := prepared.Reconciliation.CarrierAbsences()
					if len(absences) != 1 ||
						absences[0].Decision() != carrierabsence.DecisionRetireAlreadyAbsent ||
						!absences[0].Claim().ExactEqual(claim) {
						t.Fatalf("carrier absences = %#v, want exact state-only retirement", absences)
					}

					result, err := ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{
						PlanWasDisclosed: true,
					})
					if err != nil {
						t.Fatalf("ExecuteWithOptions: %v", err)
					}
					if result.ActionCount != 1 {
						t.Fatalf("ActionCount = %d, want one claim retirement", result.ActionCount)
					}
					assertApplyCarrierClaimAbsent(t, root, manifestPath, claim)

					retry, err := PlanWrite(context.Background(), CommandInput{
						ManifestPath:         manifestPath,
						LockfilePath:         lockfilePath,
						RelationObservations: &observerelation.Batch{},
					})
					if err != nil {
						t.Fatalf("retry PlanWrite: %v", err)
					}
					if len(retry.Reconciliation.CarrierAbsences()) != 0 {
						t.Fatalf(
							"retry carrier absences = %#v, want no action after exact retirement",
							retry.Reconciliation.CarrierAbsences(),
						)
					}
					retryResult, err := ExecuteWithOptions(context.Background(), retry, ExecuteOptions{
						PlanWasDisclosed: true,
					})
					if err != nil {
						t.Fatalf("retry ExecuteWithOptions: %v", err)
					}
					if retryResult.ActionCount != 0 {
						t.Fatalf("retry ActionCount = %d, want no-op", retryResult.ActionCount)
					}
				})
			}
		})
	}
}

func TestPlanWritePlansPresentClaudeCarrierRemovalRoute(t *testing.T) {
	root, manifestPath, lockfilePath, _, previous, relation := writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
	writeApplyLockfile(t, lockfilePath, lock.File{Version: lock.CurrentVersion})
	claim := seedApplyCarrierClaim(t, root, manifestPath, previous, target.ScopeProject)
	present := applyClaudeObservationBatch(t, previous, relation, applyClaudePluginCarrierInventory(
		t,
		observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				applyClaudePluginCarrierManagedRowWithScope(
					t,
					"context7@market",
					string(relation.ExpectedRelation().ManagedInstanceKey()),
					observeclaudeplugin.HostScopeProject,
				),
			},
		},
	))

	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &present,
	})
	if err != nil {
		t.Fatalf("PlanWrite: %v", err)
	}
	absences := prepared.Reconciliation.CarrierAbsences()
	if len(absences) != 1 ||
		absences[0].Decision() != carrierabsence.DecisionRemove ||
		!absences[0].Claim().ExactEqual(claim) {
		t.Fatalf("carrier absences = %#v, want exact Claude removal route", absences)
	}
	assertApplyCarrierClaimPresent(t, root, manifestPath, claim)
}

func seedApplyCarrierClaim(
	t *testing.T,
	root string,
	manifestPath string,
	previous lock.File,
	scope target.Scope,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	return seedApplyCarrierClaimWithProvenance(
		t,
		root,
		manifestPath,
		previous,
		scope,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
}

func seedApplyCarrierClaimWithProvenance(
	t *testing.T,
	root string,
	manifestPath string,
	previous lock.File,
	scope target.Scope,
	provenance durablecarrier.ClaimProvenance,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("resolve daem paths: %v", err)
	}
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(paths.StatefilePath)
	if err != nil {
		t.Fatalf("canonicalize statefile key: %v", err)
	}
	owner, err := stateauthority.New(statefileKey, manifestPath)
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	record := previous.Locked.Subjects()[0]
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest: %v", err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		provenance,
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	switch scope {
	case target.ScopeProject:
		writeApplyStatefile(t, paths.StatefilePath, applyStateSnapshot(t, durable.SnapshotInput{
			ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
		}))
	case target.ScopeGlobal:
		store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
		if err != nil {
			t.Fatalf("open carrier claim registry: %v", err)
		}
		if _, err := store.Upsert(context.Background(), claim); err != nil {
			t.Fatalf("seed global carrier claim: %v", err)
		}
	default:
		t.Fatalf("unsupported test scope %q", scope)
	}
	return claim
}

func assertApplyCarrierClaimAbsent(
	t *testing.T,
	root string,
	manifestPath string,
	claim durablecarrier.ManagedCarrierClaim,
) {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("resolve daem paths below %q: %v", root, err)
	}
	switch claim.Identity().Scope() {
	case target.ScopeProject:
		state, err := statefile.LoadOptional(context.Background(), paths.StatefilePath)
		if err != nil {
			t.Fatalf("load project statefile: %v", err)
		}
		if len(state.ManagedCarrierClaims()) != 0 {
			t.Fatalf("project carrier claims = %#v, want none", state.ManagedCarrierClaims())
		}
	case target.ScopeGlobal:
		store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
		if err != nil {
			t.Fatalf("open carrier claim registry: %v", err)
		}
		registry, err := store.Load(context.Background())
		if err != nil {
			t.Fatalf("load carrier claim registry: %v", err)
		}
		if len(registry.Claims()) != 0 {
			t.Fatalf("global carrier claims = %#v, want none", registry.Claims())
		}
	}
}

func assertApplyCarrierClaimPresent(
	t *testing.T,
	root string,
	manifestPath string,
	claim durablecarrier.ManagedCarrierClaim,
) {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("resolve daem paths below %q: %v", root, err)
	}
	state, err := statefile.LoadOptional(context.Background(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("load project statefile: %v", err)
	}
	claims := state.ManagedCarrierClaims()
	if len(claims) != 1 || !claims[0].ExactEqual(claim) {
		t.Fatalf("project carrier claims = %#v, want exact retained claim", claims)
	}
}

func TestStateOnlyCarrierClaimRetirementsRejectsInvalidAction(t *testing.T) {
	_, _, err := stateOnlyCarrierClaimRetirements([]carrierabsence.Action{{}})
	if err == nil {
		t.Fatal("stateOnlyCarrierClaimRetirements accepted a zero action")
	}
}

func TestRetireGlobalCarrierClaimsRejectsAbsentExactClaim(t *testing.T) {
	root := t.TempDir()
	registry, count, err := retireGlobalCarrierClaims(
		context.Background(),
		filepath.Join(root, "carrier-claims.json"),
		durablecarrier.EmptyGlobalCarrierClaims(),
		[]durablecarrier.ManagedCarrierClaim{{}},
	)
	if err == nil || count != 0 || !registry.Equal(durablecarrier.EmptyGlobalCarrierClaims()) {
		t.Fatalf("retireGlobalCarrierClaims = (%#v, %d, %v), want validation failure", registry, count, err)
	}
}
