package lock

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type nativeDocumentObservationCandidate struct {
	resource  hostconfig.Config
	locked    lock.LockedSubjectContract
	placement profile.HostConfigPlacement
}

// ResolveNativeDocumentSourceEpoch observes native and ordinary sources in one selected epoch.
// It grants no permission to execute the resulting observations.
func ResolveNativeDocumentSourceEpoch(ctx context.Context, paths daempaths.Paths, environment desired.Environment, locked lock.File, selection targetselection.Selection) (SourceEpoch, error) {
	if ctx == nil {
		return SourceEpoch{}, fmt.Errorf("native source observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return SourceEpoch{}, err
	}
	if err := environment.Validate(); err != nil {
		return SourceEpoch{}, err
	}
	if err := lock.ValidateNative(locked); err != nil {
		return SourceEpoch{}, err
	}
	candidates, err := nativeDocumentLockObservationCandidates(ctx, environment.HostConfigs(), locked, selection)
	if err != nil {
		return SourceEpoch{}, err
	}
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		return SourceEpoch{}, err
	}
	return resolveSourceEpochCandidates(ctx, resolver, environment, locked, selection, candidates)
}

func nativeDocumentLockObservationCandidates(ctx context.Context, resources []hostconfig.Config, locked lock.File, selection targetselection.Selection) ([]nativeDocumentObservationCandidate, error) {
	candidates := make([]nativeDocumentObservationCandidate, 0, len(resources))
	for _, resource := range resources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(selectedLockedManagedPathTargets(locked, resource.ID(), selection)) == 0 {
			continue
		}
		placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
			Target: resource.Target(), Scope: resource.Scope(), Path: resource.Path(), Ownership: resource.Ownership(), Syntax: resource.Syntax(),
		})
		if err != nil {
			return nil, err
		}
		if placement.Ownership() != hostconfig.OwnershipDocument {
			return nil, fmt.Errorf("native document observation requires document ownership")
		}
		contract, present := locked.Locked.ExactSupplySubject(resource.ID())
		if !present {
			continue
		}
		candidates = append(candidates, nativeDocumentObservationCandidate{resource: resource, locked: contract, placement: placement})
	}
	return candidates, nil
}

func nativeDocumentLockObservations(ctx context.Context, epoch SourceEpoch, candidates []nativeDocumentObservationCandidate) ([]observe.ExactSupplyObservation, error) {
	observations := make([]observe.ExactSupplyObservation, 0, len(candidates))
	for _, candidate := range candidates {
		if ctx == nil {
			return nil, fmt.Errorf("native source observation context is required")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolution, err := epoch.sourceResolution(candidate.resource.ID(), candidate.resource.Source())
		if err != nil {
			return nil, fmt.Errorf("native document %q source: %w", candidate.resource.ID(), err)
		}
		identity := resolution.Identity()
		if identity.Kind() != artifact.ArtifactKindFile {
			return nil, fmt.Errorf("native document %q: expected a file source", candidate.resource.ID())
		}
		lockedIdentity, hasIdentity := candidate.locked.ExactSupply()
		lockedPlacement, hasPlacement := candidate.locked.HostConfigPlacement()
		stale := !hasIdentity || !lockedIdentity.Equal(identity) || !hasPlacement || lockedPlacement != candidate.placement
		if !stale {
			if err := resolution.View().Verify(ctx, identity); err != nil {
				return nil, fmt.Errorf("native document %q verify source: %w", candidate.resource.ID(), err)
			}
		}
		observation, err := observe.NewExactSupplyObservation(candidate.locked.SubjectID(), stale)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	return observations, nil
}
