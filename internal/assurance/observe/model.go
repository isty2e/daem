package observe

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

// OwnershipObservation reports the durable claim, if any, overlapping one resolved output address.
type OwnershipObservation struct {
	destination output.Destination
	contentPath output.ContentPath
	address     ownership.ManagedAddress
	provisional pathauthority.Provisional
	claim       ownership.ClaimValue
}

// NewExactOwnershipObservation constructs an exact address observation with an
// optional overlapping durable claim.
func NewExactOwnershipObservation(
	destination output.Destination,
	contentPath output.ContentPath,
	address ownership.ManagedAddress,
	claim ownership.ClaimValue,
) (OwnershipObservation, error) {
	observation := OwnershipObservation{
		destination: destination, contentPath: contentPath, address: address, claim: claim,
	}
	if err := observation.Validate(); err != nil {
		return OwnershipObservation{}, err
	}
	return observation, nil
}

// NewProvisionalOwnershipObservation constructs a future-path observation
// with optional proven ancestor-claim conflict evidence. It never invents
// exact authority for the candidate.
func NewProvisionalOwnershipObservation(
	destination output.Destination,
	contentPath output.ContentPath,
	provisional pathauthority.Provisional,
	claim ownership.ClaimValue,
) (OwnershipObservation, error) {
	observation := OwnershipObservation{
		destination: destination, contentPath: contentPath, provisional: provisional,
		claim: claim,
	}
	if err := observation.Validate(); err != nil {
		return OwnershipObservation{}, err
	}
	return observation, nil
}

// Validate rejects incomplete, ambiguous, or contradictory ownership evidence.
func (observation OwnershipObservation) Validate() error {
	if err := observation.destination.Validate(); err != nil {
		return fmt.Errorf("destination: %w", err)
	}
	if err := observation.destination.ValidateScope(target.ScopeGlobal); err != nil {
		return fmt.Errorf("ownership observation destination: %w", err)
	}
	if err := observation.contentPath.Validate(); err != nil {
		return err
	}
	hasExact := !observation.address.PathAuthority().IsZero()
	hasProvisional := !observation.provisional.IsZero()
	if hasExact == hasProvisional {
		return fmt.Errorf("ownership observation must contain exactly one exact address or provisional path")
	}
	if hasExact {
		if err := observation.address.Validate(); err != nil {
			return fmt.Errorf("exact address: %w", err)
		}
		if observation.address.ContentPath() != string(observation.contentPath) {
			return fmt.Errorf("ownership observation content path disagrees with its exact address")
		}
		if claim, present := observation.claim.Get(); present {
			if err := claim.Validate(); err != nil {
				return fmt.Errorf("claim: %w", err)
			}
			if !claim.Address().Overlaps(observation.address) {
				return fmt.Errorf("ownership observation claim does not overlap its exact address")
			}
		}
		return nil
	}
	if err := observation.provisional.Validate(); err != nil {
		return err
	}
	if claim, present := observation.claim.Get(); present &&
		!observation.provisional.CandidateWithin(claim.Address().PathAuthority()) {
		return fmt.Errorf("provisional ownership observation claim does not contain its candidate")
	}
	return nil
}

// Destination returns the logical output destination associated with the observation.
func (observation OwnershipObservation) Destination() output.Destination {
	return observation.destination
}

// ContentPath returns the managed projection within the destination.
func (observation OwnershipObservation) ContentPath() output.ContentPath {
	return observation.contentPath
}

// ExactAddress returns exact durable authority when currently observable.
func (observation OwnershipObservation) ExactAddress() (ownership.ManagedAddress, bool) {
	return observation.address, !observation.address.PathAuthority().IsZero()
}

// ProvisionalPath returns future-path intent when exact authority is not yet observable.
func (observation OwnershipObservation) ProvisionalPath() (pathauthority.Provisional, bool) {
	return observation.provisional, !observation.provisional.IsZero()
}

// Claim returns the durable claim proven to conflict with this observation, if any.
func (observation OwnershipObservation) Claim() ownership.ClaimValue {
	return observation.claim
}

// Overlaps reports a proven footprint overlap. Exact and provisional authority
// are never treated as interchangeable evidence.
func (observation OwnershipObservation) Overlaps(other OwnershipObservation) bool {
	leftExact, leftIsExact := observation.ExactAddress()
	rightExact, rightIsExact := other.ExactAddress()
	if leftIsExact && rightIsExact {
		return leftExact.Overlaps(rightExact)
	}
	leftProvisional, leftIsProvisional := observation.ProvisionalPath()
	rightProvisional, rightIsProvisional := other.ProvisionalPath()
	return leftIsProvisional && rightIsProvisional &&
		leftProvisional.Equal(rightProvisional) &&
		observation.contentPath.Overlaps(other.contentPath)
}
