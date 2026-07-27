package execute

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestRejectUnsupportedActionsRejectsBlockedManagedPath(t *testing.T) {
	blocked := managedPathCapabilityPlan(t, "review", realization.PathProjectionCopy, false)

	err := RejectUnsupportedActions(blocked)
	if err == nil ||
		err.Error() != `plan contains error action for ".agents/skills/review": missing_lock: fresh exact-Supply lock observation is required` {
		t.Fatalf("RejectUnsupportedActions error = %v, want planning block detail", err)
	}
}

func TestRejectUnsupportedExecutableActionsRejectsUnsupportedPlacementMode(t *testing.T) {
	unsupported := managedPathCapabilityPlan(t, "review", realization.PathProjectionSymlink, true)

	err := RejectUnsupportedExecutableActions(unsupported)
	if err == nil || !strings.Contains(err.Error(), `uses unsupported placement mode "symlink"`) {
		t.Fatalf("RejectUnsupportedExecutableActions error = %v, want unsupported symlink mode", err)
	}
}

func TestRejectUnsupportedExecutableActionsAcceptsCanonicalManagedPathCreate(t *testing.T) {
	supported := managedPathCapabilityPlan(t, "review", realization.PathProjectionCopy, true)

	if err := RejectUnsupportedExecutableActions(supported); err != nil {
		t.Fatalf("RejectUnsupportedExecutableActions returned error: %v", err)
	}
}

func TestRejectUnsupportedActionsKeepsPlannerErrorsAheadOfCapabilityErrors(t *testing.T) {
	blocked := managedPathCapabilityPlan(t, "blocked", realization.PathProjectionCopy, false)
	unsupported := managedPathCapabilityPlan(t, "unsupported", realization.PathProjectionSymlink, true)
	combined := mustReconciliationPlan(
		t,
		append(blocked.ManagedPaths(), unsupported.ManagedPaths()...),
		nil,
	)

	err := RejectUnsupportedActions(combined)
	if err == nil || !strings.Contains(err.Error(), `missing_lock: fresh exact-Supply lock observation is required`) {
		t.Fatalf("RejectUnsupportedActions error = %v, want planner error first", err)
	}
}

func managedPathCapabilityPlan(
	t *testing.T,
	name string,
	placementMode realization.PathProjectionMode,
	includeSupplyObservation bool,
) reconcile.Result {
	t.Helper()
	const desiredSeed = "desired"
	supply := snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindSkill,
		Name:         name,
		SourceID:     artifact.SourceID("local:skills/review?mode=vendor"),
		ArtifactKind: artifact.ArtifactKindDirectory,
		ContentHash:  artifact.HashFileContent([]byte(desiredSeed)),
	})
	placements, err := profile.ManagedPathPlacementsForSelections(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetCodex},
		nil,
	)
	if err != nil || len(placements) != 1 {
		t.Fatalf("ManagedPathPlacementsFor = %#v, %v", placements, err)
	}
	destination, err := placements[0].ChildDestination(name)
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, err := profile.ManagedPathOperationRoute(placements[0], profile.OperationWrite)
	if err != nil {
		t.Fatal(err)
	}
	removeRoute, err := profile.ManagedPathOperationRoute(placements[0], profile.OperationRemove)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := placements[0].Realize(destination, placementMode, writeRoute)
	if err != nil {
		t.Fatal(err)
	}
	entityID, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(entityID, placements[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	projection, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID:      entityID,
		SubjectID:     subject,
		Realization:   spec,
		WriteRouteID:  writeRoute.RouteID(),
		RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	locked := snapshottest.Section(t, supply, projection)
	projection, _ = locked.Subject(projection.SubjectID())
	supply, _ = locked.Subject(supply.SubjectID())
	expectation, err := reconcileprojection.NewManagedPathExpectation(projection)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(
		projection.SubjectID(),
		destination,
		false,
		"",
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	var supplyObservations []observe.ExactSupplyObservation
	if includeSupplyObservation {
		observation, err := observe.NewExactSupplyObservation(supply.SubjectID(), false)
		if err != nil {
			t.Fatal(err)
		}
		supplyObservations = []observe.ExactSupplyObservation{observation}
	}
	decisions, err := reconcileprojection.BuildManagedPathDecisions(reconcileprojection.ManagedPathInput{
		Locked:             locked,
		Expectations:       []reconcileprojection.ManagedPathExpectation{expectation},
		SelectedTargets:    testSelectedTargets(t, target.TargetCodex),
		SupplyObservations: supplyObservations,
		Evidence:           []observe.ManagedPathEvidence{evidence},
	})
	if err != nil {
		t.Fatalf("BuildManagedPathDecisions returned error: %v", err)
	}
	return mustReconciliationPlan(t, decisions, nil)
}

func mustReconciliationPlan(
	t testing.TB,
	managedPaths []reconcile.ManagedPathDecision,
	aggregates []reconcile.AggregateDecision,
) reconcile.Result {
	t.Helper()
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      reconcile.ContextApply,
		ManagedPaths: managedPaths,
		Aggregates:   aggregates,
	})
	if err != nil {
		t.Fatalf("NewPlan returned error: %v", err)
	}
	return result
}
