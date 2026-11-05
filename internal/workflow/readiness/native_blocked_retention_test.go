package readiness

import (
	"bytes"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/realization/aggregate"

	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
)

func TestBlockedNativeWholeAdoptionRetainsPriorKeysAsBlockedNotRemoval(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
	input := fixture.adoption(t, false)
	whole, err := PrepareNativeWholeAdoption(t.Context(), input)
	if err != nil || !whole.Decision().IsBlocked() || len(whole.RetiredSubjects()) != 0 {
		t.Fatal("fixture did not produce a real blocked source-bound adoption", err)
	}
	subjects := append([]lock.LockedSubjectContract{input.Source.SourceContract()}, input.Source.Projections()...)
	locked, err := lock.NewLockedSection(subjects, nil)
	if err != nil {
		t.Fatal(err)
	}
	row := fixture.observation.Before.ManagedAggregates()[0]
	constraint, err := projection.NewAggregateSubjectConstraint(row.Subject(), whole.Decision().Reason(), whole.Decision().Detail())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{row.Contribution().CodecContractID()}, narrowingLimits, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := catalog.Lookup(row.Contribution().CodecContractID())
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{row.Contribution().Contract()})
	if err != nil {
		t.Fatal(err)
	}
	document, mode, present := fixture.input.Documents.Evidence().Document(fixture.observation.Aliases[0])
	if !present || mode == nil {
		t.Fatal("fixture lost its retained original")
	}
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(document, snapshot, mode.FileMode())
	if err != nil {
		t.Fatal(err)
	}
	selected, err := reconcile.NewSelectedTargets(fixture.input.Selection.Targets())
	if err != nil {
		t.Fatal(err)
	}
	reads, budget := fixture.reader.reads, fixture.observation.Budget.RemainingBytes()
	decisions, err := projection.BuildAggregateDecisions(projection.AggregateInput{
		Locked: locked, States: fixture.observation.Before.ManagedAggregates(), Constraints: []projection.AggregateSubjectConstraint{constraint},
		Evidence: []observe.AggregateEvidence{evidence}, SelectedTargets: selected, Owner: fixture.observation.Owner, Codecs: catalog,
	})
	if err != nil || len(decisions) != 1 || !decisions[0].IsBlocked() || decisions[0].MutatesHost() || decisions[0].MutatesState() {
		t.Fatal("blocked whole adoption could not preserve its prior-only key as a denial", err)
	}
	decision := decisions[0]
	if decision.Reason() != constraint.Reason() || len(decision.DesiredContributions()) != 0 || len(decision.PreviousStates()) != 1 || !decision.PreviousStates()[0].Equal(row) {
		t.Fatal("prior-only blocker invented desired state or lost the real baseline/cause")
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextInspect, ManagedPaths: []reconcile.ManagedPathDecision{whole.Decision()}, Aggregates: decisions})
	if err != nil || !result.HasErrors() || len(result.MutatingManagedPaths()) != 0 || len(result.MutatingAggregates()) != 0 {
		t.Fatal("blocked native diagnostic result became invalid or clean/mutating", err)
	}
	state, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{Before: fixture.observation.Before, ManagedPaths: result.ManagedPaths(), Aggregates: result.Aggregates(), WholeSources: []lock.LockedSubjectContract{input.Source.SourceContract()}, StateCodec: fixture.codec})
	if err == nil || state.Validate() == nil {
		t.Fatal("blocked native result exposed usable successor state")
	}
	current, err := os.ReadFile(fixture.hostPath)
	if err != nil || !bytes.Equal(current, fixture.original) || fixture.reader.reads != reads || fixture.observation.Budget.RemainingBytes() != budget || fixture.observation.Budget.RequireCapturePending() != nil {
		t.Fatal("prior-only denial reread Registry, changed allocation or modified the host", err)
	}
}
