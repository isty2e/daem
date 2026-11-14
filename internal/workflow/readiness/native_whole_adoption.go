package readiness

import (
	"context"
	"fmt"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile/build/projection"
)

type NativeWholeAdoptionInput struct {
	Source                 lockobserve.NativeSourceObservation
	Documents              journalpublication.NativeDocumentObservationBatch
	ManageUnmanagedMatches bool
}

// PrepareNativeWholeAdoption binds the actual source and retained observations
// before ordinary ownership classification or generic key retirement.
func PrepareNativeWholeAdoption(ctx context.Context, input NativeWholeAdoptionInput) (projection.NativeWholeAdoptionPlan, error) {
	if err := input.Source.Validate(); err != nil {
		return projection.NativeWholeAdoptionPlan{}, err
	}
	source := input.Source.SourceContract()
	placement, present := source.HostConfigPlacement()
	if !present || placement.Ownership() != hostconfig.OwnershipDocument {
		return projection.NativeWholeAdoptionPlan{}, fmt.Errorf("whole adoption requires an observed whole-file source")
	}
	claims, err := input.Documents.Evidence().Ownership(ctx)
	if err != nil {
		return projection.NativeWholeAdoptionPlan{}, err
	}
	alias, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
	if err != nil {
		return projection.NativeWholeAdoptionPlan{}, err
	}
	fragment, err := claims.Document(ctx, alias)
	if err != nil {
		return projection.NativeWholeAdoptionPlan{}, err
	}
	subject, err := aggregate.HostConfigDocumentSubject(source.EntityID(), placement.Target(), placement.Scope())
	if err != nil {
		return projection.NativeWholeAdoptionPlan{}, err
	}
	current, err := nativeWholePathEvidence(ctx, subject, alias, input.Documents.Evidence())
	if err != nil {
		return projection.NativeWholeAdoptionPlan{}, err
	}
	plan, err := projection.BuildNativeWholeAdoption(ctx, projection.NativeWholeAdoptionInput{
		Source: source, Before: input.Documents.Evidence().BeforeState(), Current: current,
		Ownership: fragment, ManageUnmanagedMatches: input.ManageUnmanagedMatches,
	})
	if err != nil {
		return projection.NativeWholeAdoptionPlan{}, err
	}
	if _, err := input.Documents.Evidence().Ownership(ctx); err != nil {
		return projection.NativeWholeAdoptionPlan{}, err
	}
	return plan, nil
}
