package observe

import (
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

// NewExactOwnershipObservationFromLegacy retains literal legacy scope admission.
func NewExactOwnershipObservationFromLegacy(destination output.Destination, contentPath output.ContentPath, address ownership.ManagedAddress, claim ownership.ClaimValue) (OwnershipObservation, error) {
	footprint, err := output.FootprintFromLegacy(contentPath)
	if err != nil {
		return OwnershipObservation{}, err
	}
	return NewExactOwnershipObservation(destination, footprint, address, claim)
}

func NewProvisionalOwnershipObservationFromLegacy(destination output.Destination, contentPath output.ContentPath, provisional pathauthority.Provisional, claim ownership.ClaimValue) (OwnershipObservation, error) {
	footprint, err := output.FootprintFromLegacy(contentPath)
	if err != nil {
		return OwnershipObservation{}, err
	}
	return NewProvisionalOwnershipObservation(destination, footprint, provisional, claim)
}

// LegacyContentPath refuses native names the old scope grammar cannot carry.
func (observation OwnershipObservation) LegacyContentPath() (output.ContentPath, error) {
	if err := observation.Validate(); err != nil {
		return "", err
	}
	return observation.footprint.LegacyContentPath()
}
