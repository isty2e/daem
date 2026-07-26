package ownership

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// OwnerAuthority identifies the manifest-local state authority that exclusively owns a claim.
type OwnerAuthority struct {
	statefileKey string
	manifestPath string
}

// NewOwnerAuthority validates an already-canonical statefile key and manifest provenance path.
func NewOwnerAuthority(statefileKey string, manifestPath string) (OwnerAuthority, error) {
	authority := OwnerAuthority{statefileKey: statefileKey, manifestPath: manifestPath}
	if err := authority.Validate(); err != nil {
		return OwnerAuthority{}, err
	}
	return authority, nil
}

// Validate checks authority identity without inspecting the referenced files.
func (authority OwnerAuthority) Validate() error {
	if err := validateAbsoluteCleanPath("statefile authority key", authority.statefileKey); err != nil {
		return err
	}
	if err := validateAbsoluteCleanPath("manifest provenance path", authority.manifestPath); err != nil {
		return err
	}
	return nil
}

// StatefileKey returns the canonical equality key for the state authority.
func (authority OwnerAuthority) StatefileKey() string {
	return authority.statefileKey
}

// ManifestPath returns non-authoritative provenance used for diagnostics.
func (authority OwnerAuthority) ManifestPath() string {
	return authority.manifestPath
}

// Equal reports whether two references identify the same state authority.
func (authority OwnerAuthority) Equal(other OwnerAuthority) bool {
	return authority.statefileKey == other.statefileKey
}

// IsZero reports whether no authority identity was initialized.
func (authority OwnerAuthority) IsZero() bool {
	return authority.statefileKey == "" && authority.manifestPath == ""
}

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
	owner       OwnerAuthority
	state       ClaimState
	operationID string
}

// NewReservedClaim constructs a claim tied to one interrupted-operation identity.
func NewReservedClaim(address ManagedAddress, owner OwnerAuthority, operationID string) (Claim, error) {
	claim := Claim{address: address, owner: owner, state: ClaimReserved, operationID: operationID}
	if err := claim.Validate(); err != nil {
		return Claim{}, err
	}
	return claim, nil
}

// NewActiveClaim constructs an ordinary durable ownership claim.
func NewActiveClaim(address ManagedAddress, owner OwnerAuthority) (Claim, error) {
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
		if err := validateOperationID(claim.operationID); err != nil {
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
func (claim Claim) Owner() OwnerAuthority {
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
		claim.owner.Equal(other.owner) &&
		claim.owner.manifestPath == other.owner.manifestPath &&
		claim.state == other.state &&
		claim.operationID == other.operationID
}

// OwnedBy reports whether authority is the exclusive claim owner.
func (claim Claim) OwnedBy(authority OwnerAuthority) bool {
	return claim.owner.Equal(authority)
}

// ConflictsWith reports whether another claim overlaps this claim's managed address.
func (claim Claim) ConflictsWith(other Claim) bool {
	return claim.address.Overlaps(other.address)
}

func validateAbsoluteCleanPath(name string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains a NUL byte", name)
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s %q must be absolute", name, value)
	}
	if filepath.Clean(value) != value {
		return fmt.Errorf("%s %q must be clean", name, value)
	}
	return nil
}

func validateOperationID(operationID string) error {
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
