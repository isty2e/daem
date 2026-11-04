package ownership

import (
	"context"
	"errors"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type ProtectionObservationState string

const (
	ProtectionUnobserved  ProtectionObservationState = ""
	ProtectionKnown       ProtectionObservationState = "observed"
	ProtectionUnavailable ProtectionObservationState = "unavailable"
	ProtectionUnsupported ProtectionObservationState = "unsupported"
)

// ProtectionObservation is a command's diagnostic snapshot, not a write fence,
// approval input or live assertion that an earlier record is still present.
type ProtectionObservation struct {
	state   ProtectionObservationState
	records []outputownership.PendingProtection
	cause   error
}

func KnownProtections(records []outputownership.PendingProtection) (ProtectionObservation, error) {
	for index, record := range records {
		if err := record.Validate(); err != nil {
			return ProtectionObservation{}, err
		}
		if index > 0 && !record.Owner().Equal(records[0].Owner()) {
			return ProtectionObservation{}, errors.New("owner protection observation mixes state authorities")
		}
	}

	return ProtectionObservation{state: ProtectionKnown, records: slices.Clone(records)}, nil
}

func UnavailableProtections(cause error) ProtectionObservation {
	if cause == nil {
		cause = errors.New("permission protection observation is unavailable")
	}
	return ProtectionObservation{state: ProtectionUnavailable, cause: cause}
}

func UnsupportedProtections() ProtectionObservation {
	return ProtectionObservation{state: ProtectionUnsupported}
}

func (observation ProtectionObservation) State() ProtectionObservationState { return observation.state }

func (observation ProtectionObservation) Records() []outputownership.PendingProtection {
	return slices.Clone(observation.records)
}

func (observation ProtectionObservation) Cause() error { return observation.cause }

func (observation ProtectionObservation) HasPending() bool {
	return observation.state == ProtectionKnown && len(observation.records) != 0
}

func (observation ProtectionObservation) KnownClear() bool {
	return observation.state == ProtectionKnown && len(observation.records) == 0
}

func ObservePendingProtections(ctx context.Context, paths daempaths.Paths) ProtectionObservation {
	return ObservePendingProtectionsBounded(ctx, paths, physicalwork.NewCaptureBudget(nil))
}

func ObservePendingProtectionsBounded(ctx context.Context, paths daempaths.Paths, budget rootedpath.PhysicalTraversalBudget) ProtectionObservation {
	registry, err := ownershipstore.ReadProtectionRegistryBounded(ctx, paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return UnavailableProtections(err)
	}

	var rows []outputownership.PendingProtection
	if len(registry.PendingProtections()) != 0 {
		current, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return UnavailableProtections(err)
		}
		owner, err := stateauthority.New(current.Exact(), paths.ManifestPath)
		if err != nil {
			return UnavailableProtections(err)
		}
		rows = registry.ProtectionsForOwner(owner.Key())
	}

	if err := ctx.Err(); err != nil {
		return UnavailableProtections(err)
	}

	observation, err := KnownProtections(rows)
	if err != nil {
		return UnavailableProtections(err)
	}
	return observation
}
