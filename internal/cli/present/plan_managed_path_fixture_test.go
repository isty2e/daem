package clipresent

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

type managedPathPlanFixture struct {
	locked      lock.LockedSection
	supply      lock.LockedSubjectContract
	expectation reconcileprojection.ManagedPathExpectation
	subject     lock.LockedSubjectContract
	destination output.Destination
	scope       target.Scope
	contentKind realization.PathProjectionContentKind
	permissions realization.PathPermissionPolicy
}

type managedPathPlanInput struct {
	includeDesired       bool
	selectedTargets      []target.Target
	states               []durable.ManagedPathState
	evidence             []observe.ManagedPathEvidence
	manageUnmanagedMatch bool
	owner                stateauthority.Authority
	ownership            []observe.OwnershipObservation
}

func (fixture managedPathPlanFixture) buildPlan(
	t *testing.T,
	input managedPathPlanInput,
) reconcile.Result {
	t.Helper()
	locked := snapshottest.Section(t)
	var expectations []reconcileprojection.ManagedPathExpectation
	var supplyObservations []observe.ExactSupplyObservation
	if input.includeDesired {
		locked = fixture.locked
		expectations = []reconcileprojection.ManagedPathExpectation{fixture.expectation}
		observation, err := observe.NewExactSupplyObservation(fixture.supply.SubjectID(), false)
		if err != nil {
			t.Fatal(err)
		}
		supplyObservations = []observe.ExactSupplyObservation{observation}
	}
	selectedTargets, err := reconcile.NewSelectedTargets(input.selectedTargets)
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := reconcileprojection.BuildManagedPathDecisions(reconcileprojection.ManagedPathInput{
		Locked:                 locked,
		Expectations:           expectations,
		SelectedTargets:        selectedTargets,
		SupplyObservations:     supplyObservations,
		States:                 input.states,
		Evidence:               input.evidence,
		ManageUnmanagedMatches: input.manageUnmanagedMatch,
		Owner:                  input.owner,
		Ownership:              input.ownership,
	})
	if err != nil {
		t.Fatalf("BuildManagedPathDecisions returned error: %v", err)
	}
	return mustReconciliationPlan(t, decisions, nil)
}

func (fixture managedPathPlanFixture) evidence(
	t *testing.T,
	exists bool,
	hashSeed string,
) observe.ManagedPathEvidence {
	t.Helper()
	contentHash := artifact.ContentHash("")
	if exists {
		contentHash = artifact.HashFileContent([]byte(hashSeed))
	}
	evidence, err := observe.NewManagedPathEvidence(
		fixture.subject.SubjectID(),
		fixture.destination,
		exists,
		contentHash,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

func (fixture managedPathPlanFixture) state(
	t *testing.T,
	consumers []target.Target,
	hashSeed string,
) durable.ManagedPathState {
	t.Helper()
	state, err := durable.NewManagedPathState(
		fixture.subject.SubjectID(),
		consumers,
		fixture.scope,
		fixture.destination,
		artifact.HashFileContent([]byte(hashSeed)),
		fixture.contentKind,
		fixture.permissions,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
