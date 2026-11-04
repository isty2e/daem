package projection

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
)

// BuildPositiveNativePrefix retains selected native keys and every overlapping
// transition, leaving disjoint removals and ordinary work for later.
func BuildPositiveNativePrefix(ctx context.Context, decisions []reconcile.AggregateDecision) ([]reconcile.AggregateDecision, error) {
	return buildNativeAggregatePhase(ctx, decisions, true)
}

// BuildNativeApplySuffix retains disjoint removal-only native keys and ordinary aggregate work.
func BuildNativeApplySuffix(ctx context.Context, decisions []reconcile.AggregateDecision) ([]reconcile.AggregateDecision, error) {
	return buildNativeAggregatePhase(ctx, decisions, false)
}

func buildNativeAggregatePhase(ctx context.Context, decisions []reconcile.AggregateDecision, positive bool) ([]reconcile.AggregateDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("native aggregate phase context is required")
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
			if !positive {
				result = append(result, source)
			}
			continue
		}
		if source.IsBlocked() {
			return nil, fmt.Errorf("native aggregate phase cannot use a blocked decision")
		}
		var selected []reconcile.AggregateProjectionDecision
		var contracts []aggregate.ProjectionContract
		projections := source.Projections()
		positiveRoots := nativePositivePhaseRoots(projections)
		for _, projection := range projections {
			inPrefix := false
			for _, root := range positiveRoots {
				if root.Contains(projection.Contract().Address().KeyPath()) {
					inPrefix = true
					break
				}
			}
			if inPrefix == positive {
				selected = append(selected, projection)
				contracts = append(contracts, projection.Contract())
			}
		}
		if len(selected) == 0 {
			continue
		}

		phase, err := nativeDocumentSubset(source, selected, contracts)
		if err != nil {
			return nil, err
		}
		result = append(result, phase)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func nativePositivePhaseRoots(projections []reconcile.AggregateProjectionDecision) []output.KeyPath {
	var keys, desired []output.KeyPath
	for _, projection := range projections {
		key := projection.Contract().Address().KeyPath()
		keys = append(keys, key)
		if _, present := projection.Desired(); present {
			desired = append(desired, key)
		}
	}

	// Each topmost input key covers one indivisible ancestor/descendant transition.
	var roots []output.KeyPath
	for _, root := range output.TopmostKeyPaths(keys) {
		for _, key := range desired {
			if root.Contains(key) {
				roots = append(roots, root)
				break
			}
		}
	}
	return roots
}

func nativeDocumentSubset(source reconcile.AggregateDecision, selected []reconcile.AggregateProjectionDecision, contracts []aggregate.ProjectionContract) (reconcile.AggregateDecision, error) {
	operation, ok := source.NativeOperationContract()
	if !ok {
		return reconcile.AggregateDecision{}, fmt.Errorf("native aggregate phase requires a bound operation contract")
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
	selectedOperation, err := profile.NewNativeAggregateOperationContract(catalog, selection)
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
		var desired *aggregate.ContributionSet
		if set, present := original.Desired(); present {
			desired = &set
		}
		current, _ := snapshot.State(original.Contract())
		projection := aggregateProjectionDecision{
			kind: original.Kind(), reason: original.Reason(), detail: original.Detail(),
			contract: original.Contract(), desired: desired,
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
		intent, err := aggregate.NewProjectionIntent(current, desired)
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
		codecPlan: plan, rendered: rendered, evidence: evidence, nativeOperation: &selectedOperation,
	}).canonical()
}
