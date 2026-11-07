package lock

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/nativefragment"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

type nativeDocumentObservationCandidate struct {
	resource  hostconfig.Config
	locked    lock.LockedSubjectContract
	placement configuration.Placement
	limits    configdocument.Limits
	keys      map[topology.SubjectID]lock.LockedSubjectContract
}

// ResolveNativeDocumentSourceEpoch observes native and ordinary sources in one selected epoch.
// It grants no permission to execute the resulting observations.
func ResolveNativeDocumentSourceEpoch(ctx context.Context, paths daempaths.Paths, environment desired.Environment, locked lock.File, selection targetselection.Selection, limits configdocument.Limits) (SourceEpoch, error) {
	if ctx == nil {
		return SourceEpoch{}, fmt.Errorf("native source observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return SourceEpoch{}, err
	}
	if err := limits.Validate(); err != nil {
		return SourceEpoch{}, err
	}
	if err := environment.Validate(); err != nil {
		return SourceEpoch{}, err
	}
	if err := lock.Validate(locked); err != nil {
		return SourceEpoch{}, err
	}
	candidates, err := nativeDocumentLockObservationCandidates(ctx, environment.HostConfigs(), locked, selection, limits)
	if err != nil {
		return SourceEpoch{}, err
	}
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		return SourceEpoch{}, err
	}
	return resolveSourceEpochCandidates(ctx, resolver, environment, locked, selection, candidates)
}

func nativeDocumentLockObservationCandidates(ctx context.Context, resources []hostconfig.Config, locked lock.File, selection targetselection.Selection, limits configdocument.Limits) ([]nativeDocumentObservationCandidate, error) {
	keys := make(map[entity.ID]map[topology.SubjectID]lock.LockedSubjectContract)
	for _, contract := range locked.Locked.Subjects() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if contract.EntityID().Kind() != entity.KindHostConfig {
			continue
		}
		if spec, realized := contract.Realization(); realized {
			if _, selected := spec.ManagedAggregateContribution(); selected {
				id := contract.EntityID()
				if keys[id] == nil {
					keys[id] = make(map[topology.SubjectID]lock.LockedSubjectContract)
				}
				keys[id][contract.SubjectID()] = contract
			}
		}
	}
	candidates := make([]nativeDocumentObservationCandidate, 0, len(resources))
	for _, resource := range resources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contract, present := locked.Locked.ExactSupplySubject(resource.ID())
		if !present {
			continue
		}
		lockedPlacement, placed := contract.HostConfigPlacement()
		if !placed {
			return nil, fmt.Errorf("native source has no locked placement")
		}
		if !selection.Includes(lockedPlacement.Target()) {
			continue
		}
		placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{
			Target: resource.Target(), Scope: resource.Scope(), Path: resource.Path(), Ownership: resource.Ownership(), Syntax: resource.Syntax(),
		})
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, nativeDocumentObservationCandidate{resource: resource, locked: contract, placement: placement, limits: limits, keys: keys[resource.ID()]})
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
			if candidate.placement.Ownership() == hostconfig.OwnershipKeys {
				matched, err := candidate.matchesKeyProjections(ctx, resolution)
				if err != nil {
					return nil, fmt.Errorf("native keys %q verify source: %w", candidate.resource.ID(), err)
				}
				stale = !matched
			} else if err := resolution.View().Verify(ctx, identity); err != nil {
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

func (candidate nativeDocumentObservationCandidate) matchesKeyProjections(ctx context.Context, resolution acquisition.Resolution) (bool, error) {
	content, err := directfile.ReadExact(ctx, resolution.View(), resolution.Identity())
	if err != nil {
		return false, err
	}
	items, err := nativefragment.HostConfigKeyContributions(ctx, candidate.resource, content.Bytes(), candidate.limits)
	if err != nil {
		return false, err
	}
	if len(items) != len(candidate.keys) {
		return false, nil
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		expected, err := lock.NewHostConfigKeySubjectContract(candidate.resource.ID(), item.SubjectID(), item.Contribution(), candidate.placement)
		if err != nil {
			return false, err
		}
		actual, present := candidate.keys[item.SubjectID()]
		if !present || !actual.Equal(expected) {
			return false, nil
		}
	}
	return true, ctx.Err()
}
