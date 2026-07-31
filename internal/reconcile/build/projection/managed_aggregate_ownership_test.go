package projection

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
)

func TestAggregateProjectionOwnershipRejectsUnsafeGlobalClaimStates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requester := aggregateOwnershipAuthority(t, root, "requester")
	foreign := aggregateOwnershipAuthority(t, root, "foreign")
	projection, managedState := aggregateOwnershipProjection(t)
	key, observation := aggregateOwnershipObservation(t, root, projection, ownership.NoClaim())

	tests := []struct {
		name         string
		projection   aggregateProjectionDecision
		owner        stateauthority.Authority
		observations []observe.OwnershipObservation
		wantReason   reconcile.ActionReason
	}{
		{
			name:       "missing observation",
			projection: projection,
			owner:      requester,
			wantReason: reconcile.ReasonOwnershipObservationMissing,
		},
		{
			name: "missing claim for managed state",
			projection: func() aggregateProjectionDecision {
				value := projection
				value.previous = []durable.ManagedAggregateState{managedState}
				return value
			}(),
			owner:        requester,
			observations: []observe.OwnershipObservation{observation},
			wantReason:   reconcile.ReasonOwnershipClaimMissing,
		},
		{
			name:       "foreign active claim",
			projection: projection,
			owner:      requester,
			observations: []observe.OwnershipObservation{
				aggregateClaimedObservation(t, observation, foreign, false),
			},
			wantReason: reconcile.ReasonOwnershipConflict,
		},
		{
			name: "self reserved claim",
			projection: func() aggregateProjectionDecision {
				value := projection
				value.previous = []durable.ManagedAggregateState{managedState}
				return value
			}(),
			owner: requester,
			observations: []observe.OwnershipObservation{
				aggregateClaimedObservation(t, observation, requester, true),
			},
			wantReason: reconcile.ReasonOwnershipReserved,
		},
		{
			name:       "active claim without local state",
			projection: projection,
			owner:      requester,
			observations: []observe.OwnershipObservation{
				aggregateClaimedObservation(t, observation, requester, false),
			},
			wantReason: reconcile.ReasonOwnershipStateConflict,
		},
		{
			name:       "overlapping projection",
			projection: projection,
			owner:      requester,
			observations: []observe.OwnershipObservation{
				observation,
				aggregateOverlappingObservation(t, root, key.destination),
			},
			wantReason: reconcile.ReasonOwnershipConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observations, conflicts, err := ownershipObservations(test.observations)
			if err != nil {
				t.Fatalf("ownershipObservations returned error: %v", err)
			}
			got := enforceAggregateProjectionOwnership(
				test.projection,
				test.owner,
				observations,
				conflicts,
			)
			if got.Kind() != reconcile.AggregateBlocked || got.Reason() != test.wantReason {
				t.Fatalf(
					"projection = %s/%s, want %s/%s",
					got.Kind(),
					got.Reason(),
					reconcile.AggregateBlocked,
					test.wantReason,
				)
			}
		})
	}
}

func TestAggregateProjectionOwnershipAdmitsNewAndSelfOwnedGlobalProjection(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	owner := aggregateOwnershipAuthority(t, root, "requester")
	projection, managedState := aggregateOwnershipProjection(t)
	_, observation := aggregateOwnershipObservation(t, root, projection, ownership.NoClaim())

	observations, conflicts, err := ownershipObservations([]observe.OwnershipObservation{observation})
	if err != nil {
		t.Fatal(err)
	}
	got := enforceAggregateProjectionOwnership(projection, owner, observations, conflicts)
	if got.Kind() != reconcile.AggregateCreate || got.Reason() != reconcile.ReasonMissingOutput {
		t.Fatalf("new projection = %s/%s, want create/missing_output", got.Kind(), got.Reason())
	}

	projection.previous = []durable.ManagedAggregateState{managedState}
	claimed := aggregateClaimedObservation(t, observation, owner, false)
	observations, conflicts, err = ownershipObservations([]observe.OwnershipObservation{claimed})
	if err != nil {
		t.Fatal(err)
	}
	got = enforceAggregateProjectionOwnership(projection, owner, observations, conflicts)
	if got.Kind() != reconcile.AggregateCreate || got.Reason() != reconcile.ReasonMissingOutput {
		t.Fatalf("self-owned projection = %s/%s, want preserved decision", got.Kind(), got.Reason())
	}
}

func aggregateOwnershipProjection(
	t *testing.T,
) (aggregateProjectionDecision, durable.ManagedAggregateState) {
	t.Helper()
	canonical := `{"args":[],"command":"npx","type":"stdio"}`
	locked := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID:         aggregate.MCPPlacementClaudeGlobal,
		ServerID:            "context7",
		LauncherCommand:     "npx",
		CanonicalProjection: canonical,
	})
	item, present, err := locked.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("ManagedAggregateContribution = %#v, %t, %v", item, present, err)
	}
	state, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
	if err != nil {
		t.Fatalf("NewManagedAggregateState returned error: %v", err)
	}
	return aggregateProjectionDecision{
		kind:     reconcile.AggregateCreate,
		reason:   reconcile.ReasonMissingOutput,
		contract: item.Contribution().Contract(),
		desired:  aggregateContributionSetForTest(t, item),
	}, state
}

func aggregateContributionSetForTest(
	t *testing.T,
	item aggregate.SubjectContribution,
) *aggregate.ContributionSet {
	t.Helper()
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatalf("NewContributionSet returned error: %v", err)
	}
	return &set
}

func aggregateOwnershipAuthority(
	t *testing.T,
	root string,
	name string,
) stateauthority.Authority {
	t.Helper()
	authority, err := stateauthority.New(pathtest.Exact(
		filepath.Join(root, name, "state.json"),
	),

		filepath.Join(root, name+".toml"))
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
	}
	return authority
}

func aggregateOwnershipObservation(
	t *testing.T,
	root string,
	projection aggregateProjectionDecision,
	claim ownership.ClaimValue,
) (ownershipObservationKey, observe.OwnershipObservation) {
	t.Helper()
	address := projection.contract.Address()
	document := address.Document()
	destination := document.AggregateRoot()
	contentPath := output.ContentPath(address.ContentPath())
	managedAddress, err := ownership.NewManagedAddress(pathtest.Exact(
		filepath.Join(root, "home", ".claude.json"),
	),

		string(contentPath))
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	return ownershipObservationKey{
			destination: destination,
			contentPath: contentPath,
		}, observe.OwnershipObservation{
			Destination: destination,
			ContentPath: contentPath,
			Address:     managedAddress,
			Claim:       claim,
		}
}

func aggregateClaimedObservation(
	t *testing.T,
	observation observe.OwnershipObservation,
	owner stateauthority.Authority,
	reserved bool,
) observe.OwnershipObservation {
	t.Helper()
	var claim ownership.Claim
	var err error
	if reserved {
		claim, err = ownership.NewReservedClaim(observation.Address, owner, "operation-1")
	} else {
		claim, err = ownership.NewActiveClaim(observation.Address, owner)
	}
	if err != nil {
		t.Fatalf("construct ownership claim: %v", err)
	}
	observation.Claim, _ = ownership.PresentClaim(claim)
	return observation
}

func aggregateOverlappingObservation(
	t *testing.T,
	root string,
	destination output.Destination,
) observe.OwnershipObservation {
	t.Helper()
	address, err := ownership.NewManagedAddress(pathtest.Exact(
		filepath.Join(root, "home", ".claude.json"),
	),

		"/mcpServers")
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	return observe.OwnershipObservation{
		Destination: destination,
		ContentPath: "/mcpServers",
		Address:     address,
		Claim:       ownership.NoClaim(),
	}
}
