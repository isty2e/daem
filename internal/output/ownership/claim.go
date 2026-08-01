package ownership

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

// ClaimState identifies the durable lifecycle of one ownership claim.
type ClaimState string

const (
	// ClaimReserved excludes foreign acquisition before the first host effect.
	ClaimReserved ClaimState = "reserved"
	// ClaimActive grants ordinary update and removal authority to its owner.
	ClaimActive ClaimState = "active"
)

// Claim records durable exclusive authority over one managed address.
type Claim struct {
	address     ManagedAddress
	owner       stateauthority.Authority
	state       ClaimState
	operationID string
}

// NewReservedClaim constructs a claim tied to one interrupted-operation identity.
func NewReservedClaim(address ManagedAddress, owner stateauthority.Authority, operationID string) (Claim, error) {
	claim := Claim{address: address, owner: owner, state: ClaimReserved, operationID: operationID}
	if err := claim.Validate(); err != nil {
		return Claim{}, err
	}
	return claim, nil
}

// NewActiveClaim constructs an ordinary durable ownership claim.
func NewActiveClaim(address ManagedAddress, owner stateauthority.Authority) (Claim, error) {
	claim := Claim{address: address, owner: owner, state: ClaimActive}
	if err := claim.Validate(); err != nil {
		return Claim{}, err
	}
	return claim, nil
}

// Validate checks claim state and cross-field invariants.
func (claim Claim) Validate() error {
	if err := claim.address.Validate(); err != nil {
		return fmt.Errorf("claim address: %w", err)
	}
	if err := claim.owner.Validate(); err != nil {
		return fmt.Errorf("claim owner: %w", err)
	}
	switch claim.state {
	case ClaimReserved:
		if err := ValidateOperationID(claim.operationID); err != nil {
			return err
		}
	case ClaimActive:
		if claim.operationID != "" {
			return fmt.Errorf("active ownership claim must not retain an operation id")
		}
	default:
		return fmt.Errorf("unsupported ownership claim state %q", claim.state)
	}
	return nil
}

// Address returns the claimed managed footprint.
func (claim Claim) Address() ManagedAddress {
	return claim.address
}

// Owner returns the exclusive state authority.
func (claim Claim) Owner() stateauthority.Authority {
	return claim.owner
}

// State returns the durable claim lifecycle state.
func (claim Claim) State() ClaimState {
	return claim.state
}

// OperationID returns the reservation operation identity, or empty for active claims.
func (claim Claim) OperationID() string {
	return claim.operationID
}

// Equal reports exact persisted claim equality.
func (claim Claim) Equal(other Claim) bool {
	return claim.address.Equal(other.address) &&
		claim.owner.ExactEqual(other.owner) &&
		claim.state == other.state &&
		claim.operationID == other.operationID
}

// OwnedBy reports whether authority is the exclusive claim owner.
func (claim Claim) OwnedBy(authority stateauthority.Authority) bool {
	return claim.owner.Equal(authority)
}

// ConflictsWith reports whether another claim overlaps this claim's managed address.
func (claim Claim) ConflictsWith(other Claim) bool {
	return claim.address.Overlaps(other.address)
}

// ValidateOperationID checks the operation identity shared by provisional
// acquisition intents and exact reserved claims.
func ValidateOperationID(operationID string) error {
	if strings.TrimSpace(operationID) == "" {
		return fmt.Errorf("reserved ownership claim requires an operation id")
	}
	if strings.TrimSpace(operationID) != operationID || strings.Contains(operationID, "/") || strings.Contains(operationID, "\\") {
		return fmt.Errorf("ownership claim operation id %q must be one safe path component", operationID)
	}
	if strings.IndexFunc(operationID, func(r rune) bool {
		return unicode.IsControl(r) || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.')
	}) >= 0 {
		return fmt.Errorf("ownership claim operation id contains an unsafe character")
	}
	if operationID == "." || operationID == ".." {
		return fmt.Errorf("ownership claim operation id %q is unsafe", operationID)
	}
	return nil
}
