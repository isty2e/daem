package journal

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type nativePlanningOwnershipBaseline struct {
	namespace  NativeDocumentNamespace
	comparison pathauthority.Exact
	claims     []ownership.Claim
}

// NativeOwnershipProjection answers read-only questions about the batch's
// retained Registry and qualified before facts. It neither observes again nor
// grants live state, root, source or mutation authority.
type NativeOwnershipProjection struct {
	owner       stateauthority.Authority
	registry    ownership.Registry
	before      documentSnapshotIndex
	baselines   map[aggregate.DocumentAddress]nativePlanningOwnershipBaseline
	budget      *recovery.PhysicalWorkBudget
	initialized bool
}

func (batch NativeDocumentObservationBatch) Ownership(ctx context.Context) (NativeOwnershipProjection, error) {
	if ctx == nil || !batch.initialized {
		return NativeOwnershipProjection{}, fmt.Errorf("native observation batch and context are required")
	}
	if err := errors.Join(ctx.Err(), batch.budget.RequireCapturePending()); err != nil {
		return NativeOwnershipProjection{}, err
	}
	before, err := indexDocumentSnapshot(ctx, batch.before)
	if err != nil {
		return NativeOwnershipProjection{}, err
	}
	native, err := before.nativeDestinations(ctx)
	if err != nil {
		return NativeOwnershipProjection{}, err
	}
	owned := make(map[string][]ownership.Claim)
	for _, claim := range batch.registry.Claims() {
		if err := ctx.Err(); err != nil {
			return NativeOwnershipProjection{}, err
		}
		if claim.OwnedBy(batch.owner) {
			owned[claim.Address().Path()] = append(owned[claim.Address().Path()], claim)
		}
	}
	baselines := make(map[aggregate.DocumentAddress]nativePlanningOwnershipBaseline)
	input := nativeCaptureOwnershipInput{owner: batch.owner}
	for _, namespace := range batch.namespaces {
		footprints, err := before.documentFootprints(ctx, namespace.Addresses())
		if err != nil {
			return NativeOwnershipProjection{}, err
		}
		comparison, claims, err := nativeDocumentClaimBaseline(namespace, owned, footprints)
		if err != nil {
			return NativeOwnershipProjection{}, err
		}
		bindings, err := input.documentBindings(ctx, namespace, batch.registry, native, native)
		if err != nil {
			return NativeOwnershipProjection{}, err
		}
		if comparison.IsZero() {
			for _, binding := range bindings {
				if value, present := binding.Before().Get(); present {
					comparison = value.PathAuthority()
					break
				}
			}
		}
		baseline := nativePlanningOwnershipBaseline{namespace: namespace, comparison: comparison, claims: claims}
		for _, alias := range namespace.Addresses() {
			baselines[alias] = baseline
		}
	}
	projection := NativeOwnershipProjection{owner: batch.owner, registry: batch.registry, before: before, baselines: baselines, budget: batch.budget, initialized: true}
	if err := projection.validate(ctx); err != nil {
		return NativeOwnershipProjection{}, err
	}
	return projection, nil
}

func (projection NativeOwnershipProjection) validate(ctx context.Context) error {
	if ctx == nil || !projection.initialized {
		return fmt.Errorf("native ownership projection and context are required")
	}
	return errors.Join(ctx.Err(), projection.budget.RequireCapturePending())
}

func (projection NativeOwnershipProjection) Global(ctx context.Context, alias aggregate.DocumentAddress, footprint output.Footprint) (observe.OwnershipObservation, error) {
	if err := projection.validate(ctx); err != nil {
		return observe.OwnershipObservation{}, err
	}
	baseline, present := projection.baselines[alias]
	if !present || alias.Scope() != target.ScopeGlobal {
		return observe.OwnershipObservation{}, fmt.Errorf("global ownership requires an observed global alias")
	}
	if err := footprint.Validate(); err != nil {
		return observe.OwnershipObservation{}, err
	}
	claimValue := ownership.NoClaim()
	if !baseline.comparison.IsZero() {
		address, err := ownership.NewManagedAddress(baseline.comparison, footprint)
		if err != nil {
			return observe.OwnershipObservation{}, err
		}
		if claim, found := projection.registry.Conflict(address); found {
			claimValue, _ = ownership.PresentClaim(claim)
		}
	} else {
		missing, _ := baseline.namespace.observation.Provisional()
		if claim, found := projection.registry.ProvisionalAncestorConflict(missing); found {
			claimValue, _ = ownership.PresentClaim(claim)
		}
	}
	if err := projection.validate(ctx); err != nil {
		return observe.OwnershipObservation{}, err
	}
	if exact, current := baseline.namespace.observation.Exact(); current {
		address, err := ownership.NewManagedAddress(exact, footprint)
		if err != nil {
			return observe.OwnershipObservation{}, err
		}
		return observe.NewExactOwnershipObservation(alias.AggregateRoot(), footprint, address, claimValue)
	}
	missing, _ := baseline.namespace.observation.Provisional()
	return observe.NewProvisionalOwnershipObservation(alias.AggregateRoot(), footprint, missing, claimValue)
}

func (projection NativeOwnershipProjection) Document(ctx context.Context, alias aggregate.DocumentAddress) (observe.NativeDocumentClaimObservation, error) {
	if err := projection.validate(ctx); err != nil {
		return observe.NativeDocumentClaimObservation{}, err
	}
	baseline, present := projection.baselines[alias]
	if !present || baseline.comparison.IsZero() {
		return observe.NativeDocumentClaimObservation{}, fmt.Errorf("document claim projection requires an observed comparison namespace")
	}
	whole, err := ownership.NewManagedAddress(baseline.comparison, output.WholePathFootprint())
	if err != nil {
		return observe.NativeDocumentClaimObservation{}, err
	}
	claims, err := projection.registry.Overlapping(whole)
	if err != nil {
		return observe.NativeDocumentClaimObservation{}, err
	}
	bindings, err := projection.registry.DocumentBindingsAt(baseline.comparison)
	if err != nil {
		return observe.NativeDocumentClaimObservation{}, err
	}
	aliases := baseline.namespace.Addresses()
	subjects, err := projection.before.documentSubjects(ctx, aliases)
	if err != nil {
		return observe.NativeDocumentClaimObservation{}, err
	}
	result, err := observe.NewNativeDocumentClaimObservation(observe.NativeDocumentClaimObservationInput{
		Owner: projection.owner, Aliases: aliases, Comparison: baseline.comparison,
		BeforeSubjects: subjects, BeforeClaims: baseline.claims, CurrentClaims: claims, CurrentBindings: bindings,
	})
	if err != nil {
		return observe.NativeDocumentClaimObservation{}, err
	}
	if err := projection.validate(ctx); err != nil {
		return observe.NativeDocumentClaimObservation{}, err
	}
	return result, nil
}

func (index documentSnapshotIndex) documentSubjects(ctx context.Context, aliases []aggregate.DocumentAddress) ([]topology.SubjectID, error) {
	addresses, err := documentAliasSet(ctx, aliases)
	if err != nil {
		return nil, err
	}
	seen := make(map[topology.SubjectID]struct{})
	for _, row := range index.aggregates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, selected := addresses[row.Contribution().Contract().Address().Document()]; selected {
			seen[row.Subject()] = struct{}{}
		}
	}
	for _, row := range index.paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, selected := range row.ConsumerTargets() {
			address, err := aggregate.NewDocumentAddress(selected, row.Scope(), row.Destination())
			if err != nil {
				return nil, err
			}
			if _, present := addresses[address]; present {
				seen[row.Subject()] = struct{}{}
			}
		}
	}
	subjects := make([]topology.SubjectID, 0, len(seen))
	for subject := range seen {
		subjects = append(subjects, subject)
	}
	slices.SortFunc(subjects, topology.CompareSubjectID)
	return subjects, ctx.Err()
}
