package lock

import (
	"context"
	"fmt"

	lock "github.com/isty2e/daem/internal/realization/lock"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/supply/artifact"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type instructionObservationCandidate struct {
	resource instructions.Instructions
	locked   lock.LockedSubjectContract
}

func instructionLockObservationCandidates(
	instructionResources []instructions.Instructions,
	locked lock.File,
	selection targetselection.Selection,
) []instructionObservationCandidate {
	candidates := make([]instructionObservationCandidate, 0, len(instructionResources))
	for _, resource := range instructionResources {
		if len(selectedLockedManagedPathTargets(locked, resource.ID(), selection)) == 0 {
			continue
		}

		lockedContract, ok := locked.Locked.ExactSupplySubject(resource.ID())
		if !ok {
			continue
		}
		candidates = append(candidates, instructionObservationCandidate{
			resource: resource,
			locked:   lockedContract,
		})
	}
	return candidates
}

func instructionLockObservations(
	ctx context.Context,
	epoch SourceEpoch,
	candidates []instructionObservationCandidate,
) ([]observe.ExactSupplyObservation, error) {
	observations := make([]observe.ExactSupplyObservation, 0, len(candidates))
	for _, candidate := range candidates {
		resource := candidate.resource
		resolution, err := epoch.sourceResolution(resource.ID(), resource.Source())
		if err != nil {
			return nil, fmt.Errorf("instructions %q: %w", resource.ID().Name(), err)
		}
		identity := resolution.Identity()
		if identity.Kind() != artifact.ArtifactKindFile {
			return nil, fmt.Errorf("instructions %q: validate source: expected file artifact", resource.ID().Name())
		}
		lockedIdentity, hasIdentity := candidate.locked.ExactSupply()
		stale := !hasIdentity || !lockedIdentity.Equal(identity)
		if !stale {
			if err := resolution.View().Verify(ctx, identity); err != nil {
				return nil, fmt.Errorf("instructions %q: verify source: %w", resource.ID().Name(), err)
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
