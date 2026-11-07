package reconcile

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
)

// NativeNarrowingActionInput carries preparation's admitted source facts.
// Construction checks static correlation, not source completeness or freshness.
type NativeNarrowingActionInput struct {
	Source      lock.LockedSubjectContract
	Projections []lock.LockedSubjectContract
	Baseline    durable.ManagedPathState
	Current     aggregate.Document
	Plan        *aggregate.Plan
}

// NativeNarrowingAction keeps a key-mode source, prior whole ownership and the
// retained-key plan together. Current-document admission and freshness belong
// to preparation; this value grants no mutation or recovery authority.
type NativeNarrowingAction struct {
	source   lock.LockedSubjectContract
	baseline durable.ManagedPathState
	current  aggregate.Document
	plan     *aggregate.Plan
}

func NewNativeNarrowingAction(input NativeNarrowingActionInput) (NativeNarrowingAction, error) {
	placement, present := input.Source.HostConfigPlacement()
	if !present || !placement.UsesKeyOwnership() {
		return NativeNarrowingAction{}, fmt.Errorf("narrowing requires a native key-mode source")
	}
	if _, err := profile.HostConfigOperationRoute(placement, profile.OperationWrite); err != nil {
		return NativeNarrowingAction{}, err
	}
	contracts := make([]lock.LockedSubjectContract, 1, len(input.Projections)+1)
	contracts[0] = input.Source
	contracts = append(contracts, input.Projections...)
	if _, err := lock.NewLockedSection(contracts, nil); err != nil {
		return NativeNarrowingAction{}, err
	}
	baseline := input.Baseline
	if _, err := durable.NewManagedPathState(baseline.Subject(), baseline.ConsumerTargets(), baseline.Scope(), baseline.Destination(), baseline.ContentHash(), baseline.ContentKind(), baseline.PermissionPolicy(), baseline.FileMode()); err != nil {
		return NativeNarrowingAction{}, err
	}
	expectedSubject, err := aggregate.HostConfigDocumentSubject(input.Source.EntityID(), placement.Target(), placement.Scope())
	if err != nil {
		return NativeNarrowingAction{}, err
	}
	consumers := baseline.ConsumerTargets()
	if baseline.Subject() != expectedSubject || len(consumers) != 1 || consumers[0] != placement.Target() ||
		baseline.Scope() != placement.Scope() || baseline.Destination() != placement.Destination() ||
		baseline.ContentKind() != realization.PathProjectionFile || baseline.FileMode()&0o111 != 0 || !input.Current.Exists() {
		return NativeNarrowingAction{}, fmt.Errorf("narrowing source, prior whole ownership and current document differ")
	}

	var desired []aggregate.SubjectContribution
	for _, contract := range input.Projections {
		contribution, present, err := contract.ManagedAggregateContribution()
		if err != nil {
			return NativeNarrowingAction{}, err
		}
		if !present || contract.EntityID() != input.Source.EntityID() {
			return NativeNarrowingAction{}, fmt.Errorf("narrowing projection differs from its source declaration")
		}
		desired = append(desired, contribution)
	}
	sets, err := aggregate.PartitionContributionSets(desired)
	if err != nil {
		return NativeNarrowingAction{}, err
	}
	action := NativeNarrowingAction{source: input.Source, baseline: baseline, current: input.Current}
	if len(sets) == 0 {
		if input.Plan != nil {
			return NativeNarrowingAction{}, fmt.Errorf("empty narrowing source cannot carry a selected-key plan")
		}
		return action, nil
	}
	if input.Plan == nil {
		return NativeNarrowingAction{}, fmt.Errorf("nonempty narrowing source requires a selected-key plan")
	}
	plan, err := aggregate.NewPlan(input.Plan.Before(), input.Plan.Intents())
	if err != nil {
		return NativeNarrowingAction{}, err
	}
	selection, err := plan.Before().Selection()
	if err != nil {
		return NativeNarrowingAction{}, err
	}
	codecID, native := aggregate.HostConfigCodecForSyntax(string(placement.Grammar()))
	document := selection.DocumentAddress()
	if !native || selection.CodecContractID() != codecID || document.Target() != placement.Target() ||
		document.Scope() != placement.Scope() || document.AggregateRoot() != placement.Destination() || len(plan.Intents()) != len(sets) {
		return NativeNarrowingAction{}, fmt.Errorf("narrowing plan does not cover its native source placement")
	}
	expected := make(map[aggregate.ProjectionAddress]aggregate.ContributionSet, len(sets))
	for _, set := range sets {
		expected[set.Address()] = set
	}
	for _, intent := range plan.Intents() {
		actual, retained := intent.Desired()
		wanted, supplied := expected[intent.Before().Contract().Address()]
		if !retained || !supplied || !actual.Equal(wanted) {
			return NativeNarrowingAction{}, fmt.Errorf("narrowing plan must retain exactly its source key contributions")
		}
	}
	action.plan = &plan
	return action, nil
}

func (action NativeNarrowingAction) Validate() error {
	if _, supplied := action.source.ExactSupply(); !supplied || !action.current.Exists() {
		return fmt.Errorf("native narrowing action is required")
	}
	return action.baseline.ContentHash().Validate()
}

func (action NativeNarrowingAction) SourceContract() lock.LockedSubjectContract { return action.source }

func (action NativeNarrowingAction) Baseline() durable.ManagedPathState { return action.baseline }

func (action NativeNarrowingAction) CurrentDocument() aggregate.Document { return action.current }

func (action NativeNarrowingAction) Equal(other NativeNarrowingAction) bool {
	if !action.source.Equal(other.source) || !action.baseline.Equal(other.baseline) || !action.current.Equal(other.current) {
		return false
	}
	if action.plan == nil || other.plan == nil {
		return action.plan == nil && other.plan == nil
	}
	return action.plan.Before().Equal(other.plan.Before()) && slices.EqualFunc(action.plan.Intents(), other.plan.Intents(), func(a, b aggregate.ProjectionIntent) bool {
		x, hasX := a.Desired()
		y, hasY := b.Desired()
		return a.Before().Equal(b.Before()) && hasX == hasY && (!hasX || x.Equal(y))
	})
}

func (action NativeNarrowingAction) KeyPlan() (aggregate.Plan, bool) {
	if action.plan == nil {
		return aggregate.Plan{}, false
	}
	return *action.plan, true
}

func (action NativeNarrowingAction) DesiredContributions() []aggregate.SubjectContribution {
	if action.plan == nil {
		return nil
	}
	var desired []aggregate.SubjectContribution
	for _, intent := range action.plan.Intents() {
		set, _ := intent.Desired()
		desired = append(desired, set.Contributions()...)
	}
	return desired
}
