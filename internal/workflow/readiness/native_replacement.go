package readiness

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// assessNativeReplacement assembles only the selected native-key operation.
// Its complete persistence epoch is preserved for execution and recovery.
func assessNativeReplacement(ctx context.Context, input Input) (Assessment, error) {
	selection := *input.NativeReplacement
	if err := selection.Validate(); err != nil {
		return Assessment{}, err
	}
	if input.PersistenceEpoch == nil || input.Native == nil || input.Native.Documents == nil || input.SourceEpoch == nil {
		return Assessment{}, fmt.Errorf("replacement requires retained native observation and persistence epochs")
	}
	before, err := input.PersistenceEpoch.Snapshot()
	if err != nil {
		return Assessment{}, err
	}
	for _, previous := range before.ManagedPaths() {
		if id, present := topologyprojection.EntityID(previous.Subject()); present && id == selection.Declaration() {
			return Assessment{}, fmt.Errorf("replacement cannot switch document ownership to key ownership; retire the previous ownership first")
		}
	}
	budget, err := input.PersistenceEpoch.PhysicalBudget()
	if err != nil {
		return Assessment{}, err
	}
	if budget != input.Native.Budget || !before.Equal(input.Native.Documents.BeforeState()) {
		return Assessment{}, fmt.Errorf("replacement observation differs from its complete persistence epoch")
	}
	if err := input.Native.Documents.ValidatePlanningContext(ctx, input.Native.Owner, input.Native.Registry, budget); err != nil {
		return Assessment{}, err
	}

	var source lockobserve.NativeSourceObservation
	for _, config := range input.Environment.HostConfigs() {
		if config.ID() != selection.Declaration() {
			continue
		}
		if config.Ownership() != hostconfig.OwnershipKeys {
			return Assessment{}, fmt.Errorf("replacement supports key ownership only")
		}
		source, err = input.SourceEpoch.NativeSource(ctx, config)
		if err != nil {
			return Assessment{}, err
		}
	}
	if err := source.Validate(); err != nil {
		return Assessment{}, fmt.Errorf("replacement declaration is unavailable: %w", err)
	}
	inputs := managedAggregatePlanningInputs{}
	selected := make(map[topology.SubjectID]struct{})
	selectors := selection.Selectors()
	matched := make([]bool, len(selectors))
	for _, contract := range source.Projections() {
		_, key, native := topologyprojection.HostConfigKeyIdentity(contract.SubjectID())
		if !native || !selection.Includes(key) {
			continue
		}
		for index, selector := range selectors {
			matched[index] = matched[index] || selector.Contains(key)
		}
		inputs.expected = append(inputs.expected, contract)
		selected[contract.SubjectID()] = struct{}{}
	}
	for _, present := range matched {
		if !present {
			return Assessment{}, fmt.Errorf("a replacement selector does not select a declared atomic key")
		}
	}
	if len(inputs.expected) == 0 {
		return Assessment{}, fmt.Errorf("replacement has no declared atomic keys to select")
	}
	inputs.desired, err = aggregateContributionsFromContracts(inputs.expected)
	if err != nil {
		return Assessment{}, err
	}
	for _, previous := range before.ManagedAggregates() {
		id, _, native := topologyprojection.HostConfigKeyIdentity(previous.Subject())
		if native && id == selection.Declaration() {
			expected := inputs.desired[0].Contribution().Contract()
			actual := previous.Contribution().Contract()
			if actual.Address().Document() != expected.Address().Document() || actual.CodecContractID() != expected.CodecContractID() {
				return Assessment{}, fmt.Errorf("replacement cannot change the recorded host, scope, document or grammar; retire the previous ownership first")
			}
		}
		if _, present := selected[previous.Subject()]; present {
			inputs.states = append(inputs.states, previous)
			continue
		}
		for _, desired := range inputs.desired {
			left, right := previous.Contribution().Address(), desired.Contribution().Address()
			if left.Document() == right.Document() && left.Footprint().Overlaps(right.Footprint()) {
				return Assessment{}, fmt.Errorf("replacement overlaps unselected managed ownership; reconcile or retire that ownership first")
			}
		}
	}
	facts := nativeAssessmentSources{input: input.Native, sources: []lockobserve.NativeSourceObservation{source}}
	observations, err := observeAggregateDocuments(ctx, input.Resolver, inputs.desired, inputs.states, input.Selection, input.Codecs, facts)
	if err != nil {
		return Assessment{}, err
	}
	projection, err := input.Native.Documents.Ownership(ctx)
	if err != nil {
		return Assessment{}, err
	}
	var ownership []observe.OwnershipObservation
	for _, address := range inputs.ownershipProjections() {
		if address.Document().Scope() != target.ScopeGlobal {
			continue
		}
		observed, err := projection.Global(ctx, address.Document(), address.Footprint())
		if err != nil {
			return Assessment{}, err
		}
		ownership = append(ownership, observed)
	}
	targets, err := reconcile.NewSelectedTargets(input.Selection.Targets())
	if err != nil {
		return Assessment{}, err
	}
	decisions, err := reconcileprojection.BuildNativeReplacementDecisions(reconcileprojection.AggregateInput{
		Locked: input.Lockfile.Locked, Expected: inputs.expected, Desired: inputs.desired,
		States: inputs.states, Evidence: observations.evidence, ObservationFailures: observations.failures,
		SelectedTargets: targets, Owner: input.Native.Owner, Ownership: ownership, Codecs: input.Codecs,
	}, selection)
	if err != nil {
		return Assessment{}, err
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{Context: input.Context, Aggregates: decisions})
	if err != nil {
		return Assessment{}, err
	}
	claims, err := input.PersistenceEpoch.CarrierClaims()
	if err != nil {
		return Assessment{}, err
	}
	allApproval, err := input.Native.Documents.ApprovalEvidence()
	if err != nil {
		return Assessment{}, err
	}
	return Assessment{
		StatePath: input.Paths.StatefilePath, CurrentState: before, GlobalCarrierClaims: claims,
		Reconciliation: result, AggregateEvidence: observations.evidence, AggregateFailures: observations.failures,
		Owner: input.Native.Owner, Ownership: ownership, SelectedTargets: targets,
		NativeSources: []lockobserve.NativeSourceObservation{source}, NativeApproval: selectedNativeReplacementApproval(inputs, allApproval),
		nativeRegistry: input.Native.Registry,
	}, nil
}

func selectedNativeReplacementApproval(input managedAggregatePlanningInputs, facts []journal.NativeDocumentApprovalEvidence) []journal.NativeDocumentApprovalEvidence {
	document := input.desired[0].Contribution().Address().Document()
	var result []journal.NativeDocumentApprovalEvidence
	for _, fact := range facts {
		if slices.Contains(fact.Aliases, document) {
			result = append(result, fact)
		}
	}
	return result
}
