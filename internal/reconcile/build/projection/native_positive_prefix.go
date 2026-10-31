package projection

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
)

// BuildPositiveNativePrefix retains selected native keys and their classified
// ownership transitions, leaving removal-only keys and ordinary work for later.
func BuildPositiveNativePrefix(ctx context.Context, decisions []reconcile.AggregateDecision) ([]reconcile.AggregateDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("native prefix context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var result []reconcile.AggregateDecision
	for _, source := range decisions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, native := aggregate.HostConfigSyntaxForCodec(source.CodecContractID()); !native {
			continue
		}
		if source.IsBlocked() {
			return nil, fmt.Errorf("native prefix cannot use a blocked aggregate decision")
		}
		var selected []reconcile.AggregateProjectionDecision
		var contracts []aggregate.ProjectionContract
		for _, projection := range source.Projections() {
			if _, desired := projection.Desired(); desired {
				selected = append(selected, projection)
				contracts = append(contracts, projection.Contract())
			}
		}
		if len(selected) == 0 {
			continue
		}

		prefix, err := positiveNativeDocument(source, selected, contracts)
		if err != nil {
			return nil, err
		}
		result = append(result, prefix)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func positiveNativeDocument(source reconcile.AggregateDecision, selected []reconcile.AggregateProjectionDecision, contracts []aggregate.ProjectionContract) (reconcile.AggregateDecision, error) {
	operation, ok := source.NativeOperationContract()
	if !ok {
		return reconcile.AggregateDecision{}, fmt.Errorf("native prefix requires a bound operation contract")
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		return reconcile.AggregateDecision{}, err
	}
	codec := operation.Codec()
	catalog, err := aggregate.NewCodecCatalog([]aggregate.Codec{codec})
	if err != nil {
		return reconcile.AggregateDecision{}, err
	}
	prefixOperation, err := profile.NewNativeAggregateOperationContract(catalog, selection)
	if err != nil {
		return reconcile.AggregateDecision{}, err
	}
	before := source.BeforeDocument()
	snapshot, failure := codec.Read(before, selection)
	if failure != nil {
		return reconcile.AggregateDecision{}, failure
	}

	projections := make([]aggregateProjectionDecision, 0, len(selected))
	intents := make([]aggregate.ProjectionIntent, 0, len(selected))
	for _, original := range selected {
		desired, _ := original.Desired()
		current, _ := snapshot.State(original.Contract())
		projection := aggregateProjectionDecision{
			kind: original.Kind(), reason: original.Reason(), detail: original.Detail(),
			contract: original.Contract(), desired: &desired,
			previous: original.PreviousStates(), before: current,
		}
		previous, err := aggregateStateContributionSet(projection.previous)
		if err != nil {
			return reconcile.AggregateDecision{}, err
		}
		if previous != nil {
			projection.managedBaseline, err = canonicalAggregateProjection(codec, projection.contract, *previous)
			if err != nil {
				return reconcile.AggregateDecision{}, err
			}
			projection.hasManagedBaseline = true
		}
		intent, err := aggregate.NewProjectionIntent(current, &desired)
		if err != nil {
			return reconcile.AggregateDecision{}, err
		}
		for _, subject := range original.SubjectDecisions() {
			previous, hasPrevious := subject.PreviousContribution()
			occupancy, _ := subject.ContributionOccupancy()
			projection.deltas = append(projection.deltas, aggregateSubjectDelta{
				subject: subject.Subject(), contract: subject.Contract(),
				previous: previous, hasPrevious: hasPrevious, occupancy: occupancy,
				kind: subject.Kind(), reason: subject.Reason(), detail: subject.Detail(),
				mutatesHost: subject.MutatesHost(),
			})
		}
		projections = append(projections, projection)
		intents = append(intents, intent)
	}
	plan, err := aggregate.NewPlan(snapshot, intents)
	if err != nil {
		return reconcile.AggregateDecision{}, err
	}
	rendered, failure := codec.Render(before, plan)
	if failure != nil {
		return reconcile.AggregateDecision{}, failure
	}
	for index := range projections {
		projections[index].expected, _ = rendered.Expected().State(projections[index].contract)
	}
	evidence, err := observe.NewAggregateEvidence(before, snapshot, source.Evidence().FileMode())
	if err != nil {
		return reconcile.AggregateDecision{}, err
	}
	kind, reason := classifyAggregateDocument(before, true, rendered.Document(), projections)
	if kind == reconcile.AggregateNoOp || kind == reconcile.AggregateRecord {
		rendered, err = aggregate.NewRenderedDocument(before, plan, rendered.Expected())
		if err != nil {
			return reconcile.AggregateDecision{}, err
		}
	}
	return (aggregateDecision{
		kind: kind, reason: reason,
		documentAddress: selection.DocumentAddress(), codecContractID: selection.CodecContractID(),
		projections: projections, document: before, snapshot: snapshot,
		codecPlan: plan, rendered: rendered, evidence: evidence, nativeOperation: &prefixOperation,
	}).canonical()
}
