package ownership

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

// ProvisionalAcquireIntent records one future ownership acquisition
// without granting exact path authority or reserving a durable claim.
type ProvisionalAcquireIntent struct {
	destination output.Destination
	footprint   output.Footprint
	path        pathauthority.Provisional
	owner       stateauthority.Authority
	operationID string
}

// NewProvisionalAcquireIntent constructs one operation-scoped acquisition
// intent for a normalization-sensitive future path.
func NewProvisionalAcquireIntent(
	destination output.Destination,
	footprint output.Footprint,
	path pathauthority.Provisional,
	owner stateauthority.Authority,
	operationID string,
) (ProvisionalAcquireIntent, error) {
	intent := ProvisionalAcquireIntent{
		destination: destination,
		footprint:   footprint,
		path:        path,
		owner:       owner,
		operationID: operationID,
	}
	if err := intent.Validate(); err != nil {
		return ProvisionalAcquireIntent{}, err
	}
	return intent, nil
}

// Validate rejects incomplete intent or any exact-authority substitute.
func (intent ProvisionalAcquireIntent) Validate() error {
	if err := intent.destination.Validate(); err != nil {
		return fmt.Errorf("provisional acquisition destination: %w", err)
	}
	if err := intent.footprint.Validate(); err != nil {
		return err
	}
	if err := intent.path.Validate(); err != nil {
		return fmt.Errorf("provisional acquisition path: %w", err)
	}
	if err := intent.owner.Validate(); err != nil {
		return fmt.Errorf("provisional acquisition owner: %w", err)
	}
	return ValidateOperationID(intent.operationID)
}

// AdmitAddress validates a freshly observed exact address as this intent's
// realization. It grants no durable claim by itself.
func (intent ProvisionalAcquireIntent) AdmitAddress(address ManagedAddress) error {
	if err := intent.Validate(); err != nil {
		return err
	}
	if err := address.Validate(); err != nil {
		return fmt.Errorf("promoted ownership address: %w", err)
	}
	if !address.Footprint().Equal(intent.footprint) {
		return fmt.Errorf("promoted ownership address changed footprint")
	}
	return intent.path.AdmitsExact(address.PathAuthority())
}

// Destination returns the logical output represented by the intent.
func (intent ProvisionalAcquireIntent) Destination() output.Destination {
	return intent.destination
}

// Footprint returns the canonical managed scope within the destination.
func (intent ProvisionalAcquireIntent) Footprint() output.Footprint {
	return intent.footprint
}

// Path returns the non-exact candidate and exact namespace evidence.
func (intent ProvisionalAcquireIntent) Path() pathauthority.Provisional {
	return intent.path
}

// Owner returns the state authority that may acquire the eventual exact path.
func (intent ProvisionalAcquireIntent) Owner() stateauthority.Authority {
	return intent.owner
}

// OperationID returns the interrupted-operation identity.
func (intent ProvisionalAcquireIntent) OperationID() string {
	return intent.operationID
}

// Equal reports exact intent equality without treating it as path authority.
func (intent ProvisionalAcquireIntent) Equal(other ProvisionalAcquireIntent) bool {
	return intent.destination == other.destination &&
		intent.footprint == other.footprint &&
		intent.path.Equal(other.path) &&
		intent.owner.ExactEqual(other.owner) &&
		intent.operationID == other.operationID
}
