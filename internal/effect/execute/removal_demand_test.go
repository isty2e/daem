package execute

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
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
		effect        ManagedPathEffect
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
			effect:        managedPathDemandRemove(directoryBefore),
			scope:         target.ScopeProject,
			destination:   oldDestination,
			wantRelations: 1,
			wantBefore:    true,
		},
		{
			name:          "directory replacement same destination",
			effect:        managedPathDemandReplace(directoryBefore, oldDestination),
			scope:         target.ScopeProject,
			destination:   oldDestination,
			wantRelations: 1,
			wantBefore:    true,
			wantExpected:  true,
		},
		{
			name:          "atomic file replacement has no removal",
			effect:        managedPathDemandFileReplace(fileBefore),
			scope:         target.ScopeProject,
			destination:   oldDestination,
			wantRelations: 0,
		},
		{
			name:          "relocation covers both endpoints",
			effect:        managedPathDemandReplace(directoryBefore, newDestination),
			wantRelations: 2,
		},
		{
			name:          "record has no removal",
			effect:        managedPathDemandRecord(fileBefore),
			scope:         target.ScopeProject,
			destination:   oldDestination,
			wantRelations: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set, err := removalDemandSetForExecution(
				[]ManagedPathEffect{test.effect},
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("removalDemandSetForExecution returned error: %v", err)
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

func managedPathDemandCreate(t *testing.T, destination output.Destination) ManagedPathEffect {
	t.Helper()
	return ManagedPathEffect{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
		subject:          testManagedPathEffectSubject(t, "created", "skill.project.agents"),
		consumerTargets:  []target.Target{target.TargetCodex},
		scope:            target.ScopeProject,
		destination:      destination,
		desiredHash:      testArtifactHash("created"),
		contentKind:      realization.PathProjectionDirectory,
		permissionPolicy: realization.PathPermissionsNone,
	}}}
}

func managedPathDemandRemove(previous durable.ManagedPathState) ManagedPathEffect {
	return ManagedPathEffect{remove: &managedPathRemoveEffect{facts: managedPathEffectFacts{
		subject:          previous.Subject(),
		scope:            previous.Scope(),
		destination:      previous.Destination(),
		liveHash:         previous.ContentHash(),
		contentKind:      previous.ContentKind(),
		permissionPolicy: previous.PermissionPolicy(),
		previous:         &previous,
	}}}
}

func managedPathDemandReplace(previous durable.ManagedPathState, destination output.Destination) ManagedPathEffect {
	return ManagedPathEffect{replace: &managedPathReplaceEffect{facts: managedPathEffectFacts{
		subject:          previous.Subject(),
		consumerTargets:  previous.ConsumerTargets(),
		scope:            previous.Scope(),
		destination:      destination,
		desiredHash:      testArtifactHash("replacement"),
		contentKind:      previous.ContentKind(),
		permissionPolicy: previous.PermissionPolicy(),
		previous:         &previous,
	}}}
}

func managedPathDemandFileReplace(previous durable.ManagedPathState) ManagedPathEffect {
	return ManagedPathEffect{replace: &managedPathReplaceEffect{facts: managedPathEffectFacts{
		subject:          previous.Subject(),
		consumerTargets:  previous.ConsumerTargets(),
		scope:            previous.Scope(),
		destination:      previous.Destination(),
		desiredHash:      testArtifactHash("file-replacement"),
		liveHash:         previous.ContentHash(),
		contentKind:      realization.PathProjectionFile,
		permissionPolicy: realization.PathPermissionsExact,
		desiredFileMode:  previous.FileMode(),
		liveFileMode:     previous.FileMode(),
		previous:         &previous,
	}}}
}

func managedPathDemandRecord(previous durable.ManagedPathState) ManagedPathEffect {
	return ManagedPathEffect{record: &managedPathRecordEffect{facts: managedPathEffectFacts{
		subject:          previous.Subject(),
		consumerTargets:  previous.ConsumerTargets(),
		scope:            previous.Scope(),
		destination:      previous.Destination(),
		desiredHash:      previous.ContentHash(),
		liveHash:         previous.ContentHash(),
		contentKind:      previous.ContentKind(),
		permissionPolicy: previous.PermissionPolicy(),
		desiredFileMode:  previous.FileMode(),
		liveFileMode:     previous.FileMode(),
		previous:         &previous,
	}}}
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
