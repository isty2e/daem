package lock

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/topology"
)

var ErrNativeSourceStale = errors.New("native source differs from its locked intent")

// NativeSourceObservation retains one positively matched source and its complete
// projections, including an empty key source. It is not a freshness lease or an
// execution grant.
type NativeSourceObservation struct {
	source      lock.LockedSubjectContract
	projections []lock.LockedSubjectContract
}

func (observation NativeSourceObservation) Validate() error {
	_, native := observation.source.HostConfigPlacement()
	if _, supplied := observation.source.ExactSupply(); !supplied || !native {
		return fmt.Errorf("matched native source is required")
	}
	return nil
}

func (observation NativeSourceObservation) SourceContract() lock.LockedSubjectContract {
	return observation.source
}

func (observation NativeSourceObservation) Projections() []lock.LockedSubjectContract {
	return append([]lock.LockedSubjectContract(nil), observation.projections...)
}

// DesiredWholeState projects matched intent, not observed ownership or freshness.
func (observation NativeSourceObservation) DesiredWholeState() (durable.ManagedPathState, bool, error) {
	if err := observation.Validate(); err != nil {
		return durable.ManagedPathState{}, false, err
	}
	placement, _ := observation.source.HostConfigPlacement()
	if placement.Ownership() != hostconfig.OwnershipDocument {
		return durable.ManagedPathState{}, false, nil
	}
	identity, materialized := observation.source.MaterializedFileIdentity()
	if !materialized || len(observation.projections) != 1 {
		return durable.ManagedPathState{}, true, fmt.Errorf("whole source requires its exact materialized projection")
	}
	contract := observation.projections[0]
	spec, _ := contract.Realization()
	path, present := spec.ManagedPathProjection()
	mode, exact := path.ExactPermissionMode()
	if !present || !exact {
		return durable.ManagedPathState{}, true, fmt.Errorf("whole source requires its exact path and permissions")
	}
	state, err := durable.NewManagedPathState(contract.SubjectID(), path.ConsumerTargets(), path.Scope(), path.Destination(),
		identity.ContentHash(), path.ContentKind(), path.PermissionPolicy(), mode.FileMode())
	return state, true, err
}

// NativeSource rechecks the retained view for the exact selected declaration.
// Missing selection or a different source/mode is not an empty native fragment.
func (epoch SourceEpoch) NativeSource(ctx context.Context, resource hostconfig.Config) (NativeSourceObservation, error) {
	if ctx == nil {
		return NativeSourceObservation{}, fmt.Errorf("native source observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return NativeSourceObservation{}, err
	}
	if err := resource.Validate(); err != nil {
		return NativeSourceObservation{}, err
	}
	if !epoch.initialized {
		return NativeSourceObservation{}, fmt.Errorf("native source epoch is required")
	}
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
		Target: resource.Target(), Scope: resource.Scope(), Path: resource.Path(),
		Ownership: resource.Ownership(), Syntax: resource.Syntax(),
	})
	if err != nil {
		return NativeSourceObservation{}, err
	}
	for _, candidate := range epoch.nativeDocuments {
		if candidate.resource.ID() != resource.ID() {
			continue
		}
		if candidate.resource.Source() != resource.Source() || candidate.placement != placement {
			return NativeSourceObservation{}, fmt.Errorf("native source request differs from the selected declaration")
		}
		observations, err := nativeDocumentLockObservations(ctx, epoch, []nativeDocumentObservationCandidate{candidate})
		if err != nil {
			return NativeSourceObservation{}, err
		}
		if observations[0].Stale() {
			return NativeSourceObservation{}, ErrNativeSourceStale
		}

		projections := make([]lock.LockedSubjectContract, 0, len(candidate.keys))
		if placement.Ownership() == hostconfig.OwnershipKeys {
			for _, contract := range candidate.keys {
				projections = append(projections, contract)
			}
			slices.SortFunc(projections, func(left, right lock.LockedSubjectContract) int {
				return topology.CompareSubjectID(left.SubjectID(), right.SubjectID())
			})
		} else {
			projection, err := lock.NewHostConfigDocumentSubjectContract(resource.ID(), placement)
			if err != nil {
				return NativeSourceObservation{}, err
			}
			projections = append(projections, projection)
		}
		if err := ctx.Err(); err != nil {
			return NativeSourceObservation{}, err
		}
		return NativeSourceObservation{source: candidate.locked, projections: projections}, nil
	}
	return NativeSourceObservation{}, fmt.Errorf("native declaration has no selected source observation")
}
