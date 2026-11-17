package relation_test

import (
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestOrderHandoffSlotsCannotBroadenOrdinaryForeignRemoval(t *testing.T) {
	capability := selectedCapability(t, target.TargetOpenCode, desiredextension.CarrierOpenCodePlugin)
	member := mustObservedOrderMember(t, "a", "a")
	constraint := selectedConstraint(t, capability, member)
	keyFor := func(source string) observerelation.CorrelationKey {
		t.Helper()
		subject, err := hostrelation.NewSubjectKey(source)
		if err != nil {
			t.Fatal(err)
		}
		managed, err := hostrelation.NewManagedInstanceKey("handoff/" + source)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := hostrelation.NewExpectedRelation(subject, managed)
		if err != nil {
			t.Fatal(err)
		}
		key, err := observerelation.NewCorrelationKey(member.Subject(), expected)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	before, after := keyFor("old"), keyFor("new")
	events, err := observerelation.NewOrderHandoffs([]observerelation.OrderHandoffSpec{{Before: before, After: after}})
	if err != nil {
		t.Fatal(err)
	}
	if events.ValidateDesired([]observerelation.CorrelationKey{after}) != nil || events.ValidateDesired([]observerelation.CorrelationKey{before}) == nil {
		t.Fatal("handoff did not require its exact desired expectation")
	}
	old, err := observerelation.NewObservedRelationRow(mustObservedLoadIdentity(t, "old"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := observerelation.NewObservedRelationRow(mustObservedLoadIdentity(t, "foreign"))
	if err != nil {
		t.Fatal(err)
	}
	original := selectedOriginal(t, capability, capability.NativeSequences()[0].SequenceID(), []observerelation.ObservedRelationRow{old, foreign, mustCorrelatedObservedRow(t, member)})
	if _, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, []int{0}); err == nil {
		t.Fatal("ordinary deletion removed an uncorrelated physical row")
	}
	slots := []observerelation.RetiringSequenceSlot{{OriginalSlot: 0, Correlation: before}}
	selected, err := observerelation.NewSelectedHandoffSequenceEdit(capability, original, constraint, []int{0}, slots, events)
	if err != nil {
		t.Fatal(err)
	}
	slots[0].OriginalSlot = 1
	if len(selected.SurvivingRows()) != 2 || selected.SurvivingRows()[0] != foreign || len(selected.PrecedenceChanges()) != 0 || selected.RemovedSlots()[0] != 0 || !selected.Original().SameOrdering(original) {
		t.Fatal("conditional removal changed a foreign slot, original observation or caller-owned evidence")
	}
	for _, invalid := range []struct {
		slots  []observerelation.RetiringSequenceSlot
		events observerelation.OrderHandoffs
	}{
		{[]observerelation.RetiringSequenceSlot{{OriginalSlot: 0, Correlation: before}}, observerelation.OrderHandoffs{}},
		{[]observerelation.RetiringSequenceSlot{{OriginalSlot: 1, Correlation: before}}, events},
		{[]observerelation.RetiringSequenceSlot{{OriginalSlot: 0, Correlation: before}, {OriginalSlot: 1, Correlation: before}}, events},
	} {
		if _, err := observerelation.NewSelectedHandoffSequenceEdit(capability, original, constraint, []int{0, 1}, invalid.slots, invalid.events); err == nil {
			t.Fatal("handoff widened its exact slot evidence")
		}
	}
	remaining := events.WithoutRetiring([]observerelation.CorrelationKey{before})
	if !remaining.Empty() || events.Empty() || !events.AllowsRetirement(before) {
		t.Fatal("retiring an event mutated its original or retained a consumed correlation")
	}
}
