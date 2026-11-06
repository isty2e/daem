package hostmutation

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestManagedPathSchedulePreservesPhaseAndJournalOrdinals(t *testing.T) {
	previous := managedPublicationState(t, "old", []byte("old"))
	recorded := managedPublicationState(t, "recorded", []byte("recorded"))
	removed := managedPublicationState(t, "removed", []byte("removed"))
	replaced := managedPublicationState(t, "replaced", []byte("before"))
	effects := []effectpreparation.ManagedPathEffect{
		managedPublicationEffect(t, reconcile.ManagedPathReplace, "new", &previous, []byte("new")),
		managedPublicationEffect(t, reconcile.ManagedPathRecord, "recorded", &recorded, []byte("recorded")),
		managedPublicationEffect(t, reconcile.ManagedPathRemove, "removed", &removed, nil),
		managedPublicationEffect(t, reconcile.ManagedPathCreate, "created", nil, []byte("created")),
		managedPublicationEffect(t, reconcile.ManagedPathReplace, "replaced", &replaced, []byte("after")),
	}
	schedule, err := PlanManagedPaths(effects)
	if err != nil {
		t.Fatal(err)
	}
	if schedule.MutationCount() != 6 || schedule.WriteCount() != 5 {
		t.Fatalf("schedule mutations=%d writes=%d", schedule.MutationCount(), schedule.WriteCount())
	}
	for _, test := range []struct {
		phase     ManagedPathPhase
		effects   []int
		mutations []int
	}{
		{ManagedPathPublishPhase, []int{0, 1, 3, 4}, []int{0, 2, 4, 5}},
		{ManagedPathRetirePhase, []int{0, 2}, []int{1, 3}},
	} {
		operations := slices.Collect(schedule.Operations(test.phase))
		if len(operations) != len(test.effects) {
			t.Fatalf("phase %d has %d operations, want %d", test.phase, len(operations), len(test.effects))
		}
		for index, operation := range operations {
			if operation.EffectIndex() != test.effects[index] || operation.MutationIndex() != test.mutations[index] ||
				operation.Relocates() != (test.effects[index] == 0) ||
				operation.Effect().Destination() != effects[test.effects[index]].Destination() {
				t.Fatalf("phase %d operation[%d] changed effect, journal ordinal or relocation", test.phase, index)
			}
		}
	}

	effects[0] = effects[3]
	operations := slices.Collect(schedule.Operations(ManagedPathPublishPhase))
	operations[0] = ManagedPathOperation{}
	seen := 0
	for operation := range schedule.Operations(ManagedPathPublishPhase) {
		if operation.Effect().Destination() != outputtest.Parse(t, "new") {
			t.Fatal("input or returned operation replacement mutated the retained schedule")
		}
		seen++
		break
	}
	if seen != 1 || len(slices.Collect(schedule.Operations(ManagedPathPublishPhase))) != 4 {
		t.Fatal("early iterator termination consumed or corrupted the schedule")
	}
}

func managedPublicationSubject(t *testing.T, name string) topology.SubjectID {
	t.Helper()
	owner, err := entity.New(entity.KindInstructions, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(owner, "instructions.project.agents")
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func managedPublicationState(t *testing.T, destination string, content []byte) durable.ManagedPathState {
	t.Helper()
	state, err := durable.NewManagedPathState(
		managedPublicationSubject(t, destination), []target.Target{target.TargetCodex}, target.ScopeProject,
		outputtest.Parse(t, destination), artifact.HashFileContentWithExecutable(content, false),
		realization.PathProjectionFile, realization.PathPermissionsExact, 0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func managedPublicationEffect(t *testing.T, kind reconcile.ManagedPathDecisionKind, destination string, previous *durable.ManagedPathState, content []byte) effectpreparation.ManagedPathEffect {
	t.Helper()
	subject := managedPublicationSubject(t, destination)
	input := reconcile.ManagedPathDecisionInput{
		Kind: kind, Subject: subject, ConsumerTargets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		Destination: outputtest.Parse(t, destination), DesiredHash: artifact.HashFileContentWithExecutable(content, false),
		ContentKind: realization.PathProjectionFile, PermissionPolicy: realization.PathPermissionsExact,
		DesiredFileMode: 0o600, Previous: previous,
	}
	if previous != nil {
		input.Subject = previous.Subject()
		if input.Destination == previous.Destination() {
			input.LiveHash, input.LiveFileMode = previous.ContentHash(), previous.FileMode()
		}
	}
	switch kind {
	case reconcile.ManagedPathCreate:
		input.Reason = reconcile.ReasonMissingOutput
	case reconcile.ManagedPathReplace:
		input.Reason = reconcile.ReasonContentChanged
	case reconcile.ManagedPathRecord:
		input.Reason = reconcile.ReasonStateStale
	case reconcile.ManagedPathRemove:
		input.Reason = reconcile.ReasonRemovedFromManifest
		input.DesiredHash, input.DesiredFileMode, input.ConsumerTargets = "", 0, nil
	}
	decision, err := reconcile.NewManagedPathDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	effects, err := effectpreparation.ManagedPathEffects([]reconcile.ManagedPathDecision{decision})
	if err != nil || len(effects) != 1 {
		t.Fatalf("managed publication effect admission: count=%d error=%v", len(effects), err)
	}
	return effects[0]
}
