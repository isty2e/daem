package projection

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestBuildManagedPathDecisionsCoversDesiredLifecycle(t *testing.T) {
	locked, supply, projection := managedPathLock(t, "oracle", "oracle", []target.Target{target.TargetCodex}, "desired")
	missing := managedPathEvidence(t, projection, ".agents/skills/oracle", false, "")
	current := managedPathEvidence(t, projection, ".agents/skills/oracle", true, "desired")
	state := managedPathState(t, projection, []target.Target{target.TargetCodex}, ".agents/skills/oracle", "desired")

	tests := []struct {
		name           string
		state          []durable.ManagedPathState
		evidence       observe.ManagedPathEvidence
		manageExisting bool
		wantKind       reconcile.ManagedPathDecisionKind
		wantReason     reconcile.ActionReason
	}{
		{name: "create", evidence: missing, wantKind: reconcile.ManagedPathCreate, wantReason: reconcile.ReasonMissingOutput},
		{name: "unmanaged block", evidence: current, wantKind: reconcile.ManagedPathBlocked, wantReason: reconcile.ReasonUnmanagedOutputExists},
		{name: "adopt exact", evidence: current, manageExisting: true, wantKind: reconcile.ManagedPathRecord, wantReason: reconcile.ReasonManagedExisting},
		{name: "no-op", state: []durable.ManagedPathState{state}, evidence: current, wantKind: reconcile.ManagedPathNoOp, wantReason: reconcile.ReasonAlreadyCurrent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decisions, err := BuildManagedPathDecisions(ManagedPathInput{
				Locked: locked, Expectations: []ManagedPathExpectation{managedPathExpectation(t, projection)},
				SelectedTargets:    planSelectedTargets(t, target.TargetCodex),
				SupplyObservations: []observe.ExactSupplyObservation{exactSupplyObservation(t, supply, false)},
				States:             test.state, Evidence: []observe.ManagedPathEvidence{test.evidence},
				ManageUnmanagedMatches: test.manageExisting,
			})
			if err != nil {
				t.Fatalf("BuildManagedPathDecisions returned error: %v", err)
			}
			if len(decisions) != 1 || decisions[0].Kind() != test.wantKind || decisions[0].Reason() != test.wantReason {
				t.Fatalf("decisions = %#v, want %s/%s", decisions, test.wantKind, test.wantReason)
			}
		})
	}
}

func TestManagedPathDraftReclassificationReplacesPriorSemantics(t *testing.T) {
	_, _, projection := managedPathLock(
		t,
		"oracle",
		"oracle",
		[]target.Target{target.TargetCodex},
		"desired",
	)
	input := managedPathExpectation(t, projection).decisionInput()
	input.Kind = reconcile.ManagedPathBlocked
	input.Reason = reconcile.ReasonUnmanagedOutputExists
	input.Detail = "stale detail"

	decision, err := newManagedPathCreate(input, reconcile.ReasonMissingOutput).canonical()
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind() != reconcile.ManagedPathCreate ||
		decision.Reason() != reconcile.ReasonMissingOutput ||
		decision.Detail() != "" {
		t.Fatalf(
			"reclassified decision = %s/%s %q, want create/missing_output with no detail",
			decision.Kind(),
			decision.Reason(),
			decision.Detail(),
		)
	}
}

func TestBuildManagedPathDecisionsSeparatesPartialAndFinalConsumerRemoval(t *testing.T) {
	_, _, projection := managedPathLock(
		t,
		"oracle",
		"oracle",
		[]target.Target{target.TargetCodex, target.TargetAntigravityCLI},
		"desired",
	)
	state := managedPathState(
		t,
		projection,
		[]target.Target{target.TargetAntigravityCLI, target.TargetCodex},
		".agents/skills/oracle",
		"desired",
	)
	evidence := managedPathEvidence(t, projection, ".agents/skills/oracle", true, "desired")

	partial, err := BuildManagedPathDecisions(ManagedPathInput{
		Locked: snapshottest.Section(t), SelectedTargets: planSelectedTargets(t, target.TargetAntigravityCLI),
		States: []durable.ManagedPathState{state}, Evidence: []observe.ManagedPathEvidence{evidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(partial) != 1 || partial[0].Kind() != reconcile.ManagedPathRecord || partial[0].MutatesHost() ||
		!reflect.DeepEqual(partial[0].ConsumerTargets(), []target.Target{target.TargetCodex}) {
		t.Fatalf("partial removal = %#v", partial)
	}

	final, err := BuildManagedPathDecisions(ManagedPathInput{
		Locked:          snapshottest.Section(t),
		SelectedTargets: planSelectedTargets(t, target.TargetAntigravityCLI, target.TargetCodex),
		States:          []durable.ManagedPathState{state}, Evidence: []observe.ManagedPathEvidence{evidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(final) != 1 || final[0].Kind() != reconcile.ManagedPathRemove || !final[0].MutatesHost() {
		t.Fatalf("final removal = %#v", final)
	}
}

func TestBuildManagedPathDecisionsRequiresFreshTruthForDriftAndRelocation(t *testing.T) {
	locked, supply, projection := managedPathLock(t, "oracle", "review", []target.Target{target.TargetCodex}, "new")
	state := managedPathState(t, projection, []target.Target{target.TargetCodex}, ".agents/skills/oracle", "old")
	newMissing := managedPathEvidence(t, projection, ".agents/skills/review", false, "")
	oldCurrent := managedPathEvidence(t, projection, ".agents/skills/oracle", true, "old")

	decisions, err := BuildManagedPathDecisions(ManagedPathInput{
		Locked: locked, Expectations: []ManagedPathExpectation{managedPathExpectation(t, projection)},
		SelectedTargets:    planSelectedTargets(t, target.TargetCodex),
		SupplyObservations: []observe.ExactSupplyObservation{exactSupplyObservation(t, supply, false)},
		States:             []durable.ManagedPathState{state},
		Evidence:           []observe.ManagedPathEvidence{newMissing, oldCurrent},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathReplace || decisions[0].Detail() != "managed destination changed" {
		t.Fatalf("relocation decision = %#v", decisions)
	}

	driftedOld := managedPathEvidence(t, projection, ".agents/skills/oracle", true, "external")
	decisions, err = BuildManagedPathDecisions(ManagedPathInput{
		Locked: locked, Expectations: []ManagedPathExpectation{managedPathExpectation(t, projection)},
		SelectedTargets:    planSelectedTargets(t, target.TargetCodex),
		SupplyObservations: []observe.ExactSupplyObservation{exactSupplyObservation(t, supply, false)},
		States:             []durable.ManagedPathState{state},
		Evidence:           []observe.ManagedPathEvidence{newMissing, driftedOld},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathBlocked || decisions[0].Reason() != reconcile.ReasonDriftedOutput {
		t.Fatalf("drift decision = %#v", decisions)
	}
}

func TestBuildManagedPathDecisionsCorrelatesExactCrossPlacementRelocation(t *testing.T) {
	_, _, previousProjection := managedPathLockAt(
		t,
		"oracle",
		"oracle",
		[]target.Target{target.TargetOpenCode},
		nil,
		"same",
	)
	locked, supply, desiredProjection := managedPathLockAt(
		t,
		"oracle",
		"oracle",
		[]target.Target{target.TargetOpenCode},
		map[target.Target]string{target.TargetOpenCode: ".agents/skills"},
		"same",
	)
	state := managedPathState(
		t,
		previousProjection,
		[]target.Target{target.TargetOpenCode},
		".opencode/skills/oracle",
		"same",
	)
	decisions, err := BuildManagedPathDecisions(ManagedPathInput{
		Locked: locked, Expectations: []ManagedPathExpectation{managedPathExpectation(t, desiredProjection)},
		SelectedTargets:    planSelectedTargets(t, target.TargetOpenCode),
		SupplyObservations: []observe.ExactSupplyObservation{exactSupplyObservation(t, supply, false)},
		States:             []durable.ManagedPathState{state},
		Evidence: []observe.ManagedPathEvidence{
			managedPathEvidence(t, previousProjection, ".opencode/skills/oracle", true, "same"),
			managedPathEvidence(t, desiredProjection, ".agents/skills/oracle", false, ""),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathReplace {
		t.Fatalf("decisions = %#v, want one relocation replace", decisions)
	}
	previous, ok := decisions[0].PreviousState()
	if !ok || previous.Subject() != previousProjection.SubjectID() ||
		decisions[0].Subject() != desiredProjection.SubjectID() {
		t.Fatalf("cross-placement relocation = %#v previous=%#v", decisions[0], previous)
	}
}

func TestReconcileManagedPathDesiredKeepsUnsupportedPlacementModePending(t *testing.T) {
	_, _, projection := managedPathLock(t, "oracle", "oracle", []target.Target{target.TargetCodex}, "desired")
	state := managedPathState(t, projection, []target.Target{target.TargetCodex}, ".agents/skills/oracle", "desired")
	current := managedPathEvidence(t, projection, ".agents/skills/oracle", true, "desired")
	facts := reconcile.ManagedPathDecisionInput{
		Subject: projection.SubjectID(), ConsumerTargets: []target.Target{target.TargetCodex},
		Scope: target.ScopeProject, Destination: outputtest.Parse(t, ".agents/skills/oracle"),
		DesiredHash: state.ContentHash(), ContentKind: realization.PathProjectionDirectory,
		PlacementMode: realization.PathProjectionSymlink,
	}

	decision := reconcileManagedPathDesired(facts, current, state, true, nil, false)
	if decision.Kind() != reconcile.ManagedPathReplace || decision.Reason() != reconcile.ReasonContentChanged ||
		decision.Detail() != "current path kind cannot satisfy the desired placement mode" {
		t.Fatalf("decision = %#v, want pending replacement for unsupported placement mode", decision)
	}
}

func TestManagedPathDecisionInvolvesCurrentAndPreviousScopes(t *testing.T) {
	_, _, projection := managedPathLock(t, "oracle", "oracle", []target.Target{target.TargetCodex}, "desired")
	previous := managedPathState(t, projection, []target.Target{target.TargetCodex}, ".agents/skills/oracle", "desired")
	facts := reconcile.ManagedPathDecisionInput{
		Subject: projection.SubjectID(), ConsumerTargets: []target.Target{target.TargetCodex},
		Scope: target.ScopeGlobal, Destination: outputtest.Parse(t, "~/.agents/skills/oracle"), Previous: &previous,
	}
	decision := newManagedPathReplace(facts, reconcile.ReasonContentChanged, "managed destination changed")

	if !decision.InvolvesScope(target.ScopeProject) || !decision.InvolvesScope(target.ScopeGlobal) {
		t.Fatalf("relocation scopes: project=%t global=%t", decision.InvolvesScope(target.ScopeProject), decision.InvolvesScope(target.ScopeGlobal))
	}
}

func TestBuildManagedPathDecisionsBlocksMissingOrStaleSupplyObservation(t *testing.T) {
	locked, supply, projection := managedPathLock(t, "oracle", "oracle", []target.Target{target.TargetCodex}, "desired")
	evidence := managedPathEvidence(t, projection, ".agents/skills/oracle", false, "")
	for _, observations := range [][]observe.ExactSupplyObservation{
		nil,
		{exactSupplyObservation(t, supply, true)},
	} {
		decisions, err := BuildManagedPathDecisions(ManagedPathInput{
			Locked: locked, Expectations: []ManagedPathExpectation{managedPathExpectation(t, projection)},
			SelectedTargets:    planSelectedTargets(t, target.TargetCodex),
			SupplyObservations: observations, Evidence: []observe.ManagedPathEvidence{evidence},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathBlocked {
			t.Fatalf("lock evidence decisions = %#v", decisions)
		}
	}
}

func TestBuildManagedPathDecisionsRejectsMissingStaleAndUnexpectedProjectionContracts(t *testing.T) {
	locked, supply, _ := managedPathLock(t, "oracle", "locked", []target.Target{target.TargetCodex}, "desired")
	_, _, manifestProjection := managedPathLock(t, "oracle", "manifest", []target.Target{target.TargetCodex}, "desired")

	tests := []struct {
		name         string
		locked       lock.LockedSection
		expectations []ManagedPathExpectation
		wantReason   reconcile.ActionReason
	}{
		{
			name:         "missing projection",
			locked:       snapshottest.Section(t, supply),
			expectations: []ManagedPathExpectation{managedPathExpectation(t, manifestProjection)},
			wantReason:   reconcile.ReasonMissingLock,
		},
		{
			name:         "stale projection contract",
			locked:       locked,
			expectations: []ManagedPathExpectation{managedPathExpectation(t, manifestProjection)},
			wantReason:   reconcile.ReasonStaleLock,
		},
		{
			name:       "unexpected locked projection",
			locked:     locked,
			wantReason: reconcile.ReasonUnexpectedLockSubject,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decisions, err := BuildManagedPathDecisions(ManagedPathInput{
				Locked: test.locked, Expectations: test.expectations,
				SelectedTargets:    planSelectedTargets(t, target.TargetCodex),
				SupplyObservations: []observe.ExactSupplyObservation{exactSupplyObservation(t, supply, false)},
			})
			if err != nil {
				t.Fatalf("BuildManagedPathDecisions returned error: %v", err)
			}
			if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathBlocked || decisions[0].Reason() != test.wantReason {
				t.Fatalf("decisions = %#v, want blocked/%s", decisions, test.wantReason)
			}
		})
	}
}

func TestBuildManagedPathDecisionsBlocksCrossSubjectAddressTransfer(t *testing.T) {
	_, _, previousProjection := managedPathLock(t, "oracle", "shared", []target.Target{target.TargetCodex}, "desired")
	locked, supply, desiredProjection := managedPathLock(t, "review", "shared", []target.Target{target.TargetAntigravityCLI}, "desired")
	previous := managedPathState(
		t,
		previousProjection,
		[]target.Target{target.TargetCodex},
		".agents/skills/shared",
		"desired",
	)
	evidence := []observe.ManagedPathEvidence{
		managedPathEvidence(t, desiredProjection, ".agents/skills/shared", true, "desired"),
		managedPathEvidence(t, previousProjection, ".agents/skills/shared", true, "desired"),
	}

	decisions, err := BuildManagedPathDecisions(ManagedPathInput{
		Locked:                 locked,
		Expectations:           []ManagedPathExpectation{managedPathExpectation(t, desiredProjection)},
		SelectedTargets:        planSelectedTargets(t, target.TargetAntigravityCLI),
		SupplyObservations:     []observe.ExactSupplyObservation{exactSupplyObservation(t, supply, false)},
		States:                 []durable.ManagedPathState{previous},
		Evidence:               evidence,
		ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathBlocked ||
		decisions[0].Reason() != reconcile.ReasonDestinationConflict {
		t.Fatalf("cross-subject transfer decisions = %#v, want one destination-conflict block", decisions)
	}
}

func exactSupplyObservation(
	t *testing.T,
	supply lock.LockedSubjectContract,
	stale bool,
) observe.ExactSupplyObservation {
	t.Helper()
	observation, err := observe.NewExactSupplyObservation(supply.SubjectID(), stale)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func managedPathExpectation(t *testing.T, contract lock.LockedSubjectContract) ManagedPathExpectation {
	t.Helper()
	expectation, err := NewManagedPathExpectation(contract)
	if err != nil {
		t.Fatalf("NewManagedPathExpectation returned error: %v", err)
	}
	return expectation
}

func planSelectedTargets(t *testing.T, values ...target.Target) reconcile.SelectedTargets {
	t.Helper()
	selected, err := reconcile.NewSelectedTargets(values)
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func managedPathLock(
	t *testing.T,
	name string,
	installName string,
	consumers []target.Target,
	hashSeed string,
) (lock.LockedSection, lock.LockedSubjectContract, lock.LockedSubjectContract) {
	return managedPathLockAt(t, name, installName, consumers, nil, hashSeed)
}

func managedPathLockAt(
	t *testing.T,
	name string,
	installName string,
	consumers []target.Target,
	requestedRoots map[target.Target]string,
	hashSeed string,
) (lock.LockedSection, lock.LockedSubjectContract, lock.LockedSubjectContract) {
	t.Helper()
	supply := snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind: entity.KindSkill, Name: name,
		SourceID:     artifact.SourceID("local:skills/" + name + "?mode=vendor"),
		ArtifactKind: artifact.ArtifactKindDirectory,
		ContentHash:  artifact.HashFileContent([]byte(hashSeed)),
	})
	placements, err := profile.ManagedPathPlacementsForSelections(
		entity.KindSkill,
		target.ScopeProject,
		consumers,
		requestedRoots,
	)
	if err != nil || len(placements) != 1 {
		t.Fatalf("ManagedPathPlacementsForSelections = %#v, %v", placements, err)
	}
	placement := placements[0]
	destination, err := placement.ChildDestination(installName)
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		t.Fatal(err)
	}
	removeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationRemove)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := placement.Realize(destination, realization.PathProjectionCopy, writeRoute)
	if err != nil {
		t.Fatal(err)
	}
	entityID, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(entityID, placement.ID())
	if err != nil {
		t.Fatal(err)
	}
	projection, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID: entityID, SubjectID: subject, Realization: spec,
		WriteRouteID: writeRoute.RouteID(), RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshottest.Section(t, supply, projection), supply, projection
}

func managedPathEvidence(
	t *testing.T,
	projection lock.LockedSubjectContract,
	destination string,
	exists bool,
	hashSeed string,
) observe.ManagedPathEvidence {
	t.Helper()
	hash := artifact.ContentHash("")
	if exists {
		hash = artifact.HashFileContent([]byte(hashSeed))
	}
	evidence, err := observe.NewManagedPathEvidence(projection.SubjectID(), outputtest.Parse(t, destination), exists, hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

func managedPathState(
	t *testing.T,
	projection lock.LockedSubjectContract,
	consumers []target.Target,
	destination string,
	hashSeed string,
) durable.ManagedPathState {
	t.Helper()
	state, err := durable.NewManagedPathState(
		projection.SubjectID(),
		consumers,
		target.ScopeProject,
		outputtest.Parse(t, destination),
		artifact.HashFileContent([]byte(hashSeed)),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
