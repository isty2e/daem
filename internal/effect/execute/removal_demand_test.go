package execute

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRemovalDemandSetCoversManagedPathTransitionMatrix(t *testing.T) {
	oldDestination := outputtest.Parse(t, ".agents/skills/runner")
	newDestination := outputtest.Parse(t, ".agents/skills-v2/runner")
	directoryBefore := testManagedPathEffectState(t, "runner", oldDestination)
	fileBefore := testManagedPathFileState(t, "runner-file", oldDestination)

	tests := []struct {
		name          string
		effect        effectpreparation.ManagedPathEffect
		scope         target.Scope
		destination   output.Destination
		wantRelations int
		wantBefore    bool
		wantExpected  bool
	}{
		{
			name:          "directory create rollback",
			effect:        managedPathDemandCreate(t, oldDestination),
			scope:         target.ScopeProject,
			destination:   oldDestination,
			wantRelations: 1,
			wantExpected:  true,
		},
		{
			name:          "managed directory removal",
			effect:        managedPathDemandRemove(t, directoryBefore),
			scope:         target.ScopeProject,
			destination:   oldDestination,
			wantRelations: 1,
			wantBefore:    true,
		},
		{
			name:          "directory replacement same destination",
			effect:        managedPathDemandReplace(t, directoryBefore, oldDestination),
			scope:         target.ScopeProject,
			destination:   oldDestination,
			wantRelations: 1,
			wantBefore:    true,
			wantExpected:  true,
		},
		{
			name:          "atomic file replacement has no removal",
			effect:        managedPathDemandFileReplace(t, fileBefore),
			scope:         target.ScopeProject,
			destination:   oldDestination,
			wantRelations: 0,
		},
		{
			name:          "relocation covers both endpoints",
			effect:        managedPathDemandReplace(t, directoryBefore, newDestination),
			wantRelations: 2,
		},
		{
			name:          "record has no removal",
			effect:        managedPathDemandRecord(t, fileBefore),
			scope:         target.ScopeProject,
			destination:   oldDestination,
			wantRelations: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set, err := removaloperation.EffectDemands(
				[]effectpreparation.ManagedPathEffect{test.effect},
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("removaloperation.EffectDemands returned error: %v", err)
			}
			if set.Len() != test.wantRelations {
				t.Fatalf("demand relation count = %d, want %d", set.Len(), test.wantRelations)
			}
			if test.wantRelations == 0 {
				return
			}
			if test.name == "relocation covers both endpoints" {
				assertRemovalDemandStateShape(t, set, target.ScopeProject, oldDestination, true, false)
				assertRemovalDemandStateShape(t, set, target.ScopeProject, newDestination, false, true)
				return
			}
			assertRemovalDemandStateShape(t, set, test.scope, test.destination, test.wantBefore, test.wantExpected)
		})
	}
}

func assertRemovalDemandStateShape(
	t *testing.T,
	set recovery.RemovalDemandSet,
	scope target.Scope,
	destination output.Destination,
	wantBefore bool,
	wantExpected bool,
) {
	t.Helper()
	var demand recovery.RemovalDemand
	var present bool
	for _, candidate := range set.Demands() {
		if candidate.Scope() == scope && candidate.Destination() == destination {
			demand, present = candidate, true
			break
		}
	}
	if !present {
		t.Fatalf("demand for %s/%q is missing", scope, destination)
	}
	var before, expected bool
	for _, state := range demand.States() {
		if _, present := state.Before(); present {
			before = true
		}
		if _, present := state.Expected(); present {
			expected = true
		}
	}
	if before != wantBefore || expected != wantExpected {
		t.Fatalf("demand states = before:%t expected:%t, want before:%t expected:%t", before, expected, wantBefore, wantExpected)
	}
}

func managedPathDemandCreate(t *testing.T, destination output.Destination) effectpreparation.ManagedPathEffect {
	t.Helper()
	return managedPathEffectForTest(t, reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathCreate, Subject: testManagedPathEffectSubject(t, "created", "skill.project.agents"),
		ConsumerTargets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		Destination: destination, DesiredHash: testArtifactHash("created"),
		ContentKind: realization.PathProjectionDirectory, PermissionPolicy: realization.PathPermissionsNone,
	})
}

func managedPathDemandRemove(t *testing.T, previous durable.ManagedPathState) effectpreparation.ManagedPathEffect {
	t.Helper()
	return managedPathEffectForTest(t, reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathRemove, Subject: previous.Subject(), Scope: previous.Scope(),
		Destination: previous.Destination(), LiveHash: previous.ContentHash(),
		ContentKind: previous.ContentKind(), PermissionPolicy: previous.PermissionPolicy(), Previous: &previous,
	})
}

func managedPathDemandReplace(t *testing.T, previous durable.ManagedPathState, destination output.Destination) effectpreparation.ManagedPathEffect {
	t.Helper()
	input := reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathReplace, Subject: previous.Subject(), ConsumerTargets: previous.ConsumerTargets(),
		Scope: previous.Scope(), Destination: destination, DesiredHash: testArtifactHash("replacement"),
		ContentKind: previous.ContentKind(), PermissionPolicy: previous.PermissionPolicy(), Previous: &previous,
	}
	if destination == previous.Destination() {
		input.LiveHash = previous.ContentHash()
	}
	return managedPathEffectForTest(t, input)
}

func managedPathDemandFileReplace(t *testing.T, previous durable.ManagedPathState) effectpreparation.ManagedPathEffect {
	t.Helper()
	return managedPathEffectForTest(t, reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathReplace, Subject: previous.Subject(), ConsumerTargets: previous.ConsumerTargets(),
		Scope: previous.Scope(), Destination: previous.Destination(), DesiredHash: testArtifactHash("file-replacement"),
		LiveHash: previous.ContentHash(), ContentKind: realization.PathProjectionFile,
		PermissionPolicy: realization.PathPermissionsExact, DesiredFileMode: previous.FileMode(), LiveFileMode: previous.FileMode(), Previous: &previous,
	})
}

func managedPathDemandRecord(t *testing.T, previous durable.ManagedPathState) effectpreparation.ManagedPathEffect {
	t.Helper()
	return managedPathEffectForTest(t, reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathRecord, Subject: previous.Subject(), ConsumerTargets: previous.ConsumerTargets(),
		Scope: previous.Scope(), Destination: previous.Destination(), DesiredHash: previous.ContentHash(),
		LiveHash: previous.ContentHash(), ContentKind: previous.ContentKind(), PermissionPolicy: previous.PermissionPolicy(),
		DesiredFileMode: previous.FileMode(), LiveFileMode: previous.FileMode(), Previous: &previous,
	})
}

func testManagedPathFileState(t *testing.T, name string, destination output.Destination) durable.ManagedPathState {
	t.Helper()
	subject := testManagedPathEffectSubject(t, name, "skill.project.agents")
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		testArtifactHash("old-file"),
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o600,
	)
	if err != nil {
		t.Fatalf("NewManagedPathState(file) returned error: %v", err)
	}
	return state
}
