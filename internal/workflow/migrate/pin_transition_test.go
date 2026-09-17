package migrate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestStateMigrationAndRecoveryPreservePendingPiPins(t *testing.T) {
	for _, prefix := range []int{2, 4} {
		t.Run(fmt.Sprintf("prefix_%d", prefix), func(t *testing.T) {
			paths, legacy := migrationFixture(t)
			_, foreign, _ := seedClaims(t, paths, legacy)
			from, err := authorityFor(legacy)
			if err != nil {
				t.Fatal(err)
			}
			pending := map[target.Scope]carrier.ManagedCarrierClaim{}
			for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
				before := migrationPiPinClaim(t, from, scope, strings.Repeat("a", 40))
				after := migrationPiPinClaim(t, from, scope, strings.Repeat("b", 40))
				transition, err := carrier.NewPinTransition(before, after.Identity(), after.InstallRequest())
				if err != nil {
					t.Fatal(err)
				}
				pending[scope], err = transition.PendingClaim()
				if err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedCarrierClaims: []carrier.ManagedCarrierClaim{pending[target.ScopeProject]}})
			if err != nil {
				t.Fatal(err)
			}
			content, err := statefile.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, legacy.StatefilePath, content)
			global, err := carrier.NewGlobalCarrierClaims([]carrier.ManagedCarrierClaim{pending[target.ScopeGlobal], foreign})
			if err != nil {
				t.Fatal(err)
			}
			content, err = carrierclaim.Marshal(global)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, paths.CarrierClaimRegistryPath, content)

			interruptMigration(t, paths, legacy, prefix)
			recovery, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath, Recover: true})
			if err != nil {
				t.Fatal(err)
			}
			defer recovery.Close()
			if _, err := recovery.Execute(t.Context()); err != nil {
				t.Fatal(err)
			}
			statePath := legacy.StatefilePath
			owner := from
			if prefix == 4 {
				statePath = paths.StatefilePath
				owner, err = authorityFor(paths)
				if err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err = statefile.Load(t.Context(), statePath)
			if err != nil {
				t.Fatal(err)
			}
			store, err := carrierclaim.New(paths.CarrierClaimRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			global, err = store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.ManagedCarrierClaims()) != 1 || len(global.Claims()) != 2 {
				t.Fatal("migration dropped management")
			}
			claims := append(snapshot.ManagedCarrierClaims(), global.Claims()...)
			foreignFound := false
			for _, claim := range claims {
				if claim.ExactEqual(foreign) {
					foreignFound = true
					continue
				}
				expected, err := pending[claim.Identity().Scope()].WithOwner(owner)
				if err != nil {
					t.Fatal(err)
				}
				if !claim.ExactEqual(expected) {
					t.Fatal("migration or recovery changed the pending pair beyond owner authority")
				}
				if err := claim.RequireStable(); err == nil {
					t.Fatal("migration erased native uncertainty")
				}
			}
			if !foreignFound {
				t.Fatal("foreign management changed")
			}
		})
	}
}

func migrationPiPinClaim(t *testing.T, owner stateauthority.Authority, scope target.Scope, pin string) carrier.ManagedCarrierClaim {
	t.Helper()
	raw := "git:github.com/example/package@" + pin
	source, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, raw)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{Name: string(scope) + "-tools", Carrier: desiredextension.CarrierPiPackage, Target: target.TargetPi, Scope: scope, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	relation, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	key, err := hostrelation.NewSubjectKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(value.ID(), value.CarrierKey(), relation, key)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := carrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil {
		t.Fatal(err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := carrier.NewManagedCarrierClaim(owner, identity, request, carrier.ClaimProvenanceExplicitlyAdoptedObserved)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}
