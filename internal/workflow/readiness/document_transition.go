package readiness

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/build/documenttransition"
)

type NativeNarrowingActionInput struct {
	Source         lockobserve.NativeSourceObservation
	Baseline       durable.ManagedPathState
	Current        aggregate.Document
	DocumentLimits configdocument.Limits
	ValueLimits    configdocument.Limits
}

// BuildNativeNarrowingAction derives retained intent from an observed key source,
// rather than accepting a caller-selected desired list or expected image.
func BuildNativeNarrowingAction(ctx context.Context, input NativeNarrowingActionInput) (reconcile.NativeNarrowingAction, error) {
	if err := input.Source.Validate(); err != nil {
		return reconcile.NativeNarrowingAction{}, err
	}
	source := input.Source.SourceContract()
	placement, present := source.HostConfigPlacement()
	if !present || placement.Ownership() != hostconfig.OwnershipKeys {
		return reconcile.NativeNarrowingAction{}, fmt.Errorf("narrowing requires an observed key-mode source")
	}
	codecID, native := aggregate.HostConfigCodecForSyntax(string(placement.Grammar()))
	if !native {
		return reconcile.NativeNarrowingAction{}, fmt.Errorf("narrowing source has no native key codec")
	}
	projections := input.Source.Projections()
	desired := make([]aggregate.SubjectContribution, 0, len(projections))
	for _, contract := range projections {
		contribution, present, err := contract.ManagedAggregateContribution()
		if err != nil {
			return reconcile.NativeNarrowingAction{}, err
		}
		if !present {
			return reconcile.NativeNarrowingAction{}, fmt.Errorf("narrowing source contains a non-key projection")
		}
		desired = append(desired, contribution)
	}
	sets, err := aggregate.PartitionContributionSets(desired)
	if err != nil {
		return reconcile.NativeNarrowingAction{}, err
	}
	narrowing, err := documenttransition.BuildNativeDocumentNarrowing(ctx, documenttransition.NativeDocumentNarrowingInput{
		Baseline: input.Baseline, Current: input.Current, Desired: sets, CodecID: codecID,
		DocumentLimits: input.DocumentLimits, ValueLimits: input.ValueLimits,
	})
	if err != nil {
		return reconcile.NativeNarrowingAction{}, err
	}
	var plan *aggregate.Plan
	if selected, present := narrowing.KeyPlan(); present {
		plan = &selected
	}
	action, err := reconcile.NewNativeNarrowingAction(reconcile.NativeNarrowingActionInput{
		Source: source, Projections: projections, Baseline: input.Baseline, Current: input.Current, Plan: plan,
	})
	if err != nil {
		return reconcile.NativeNarrowingAction{}, err
	}
	if err := ctx.Err(); err != nil {
		return reconcile.NativeNarrowingAction{}, err
	}
	return action, nil
}
