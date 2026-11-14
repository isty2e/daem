package configuration

import (
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type ProtectionFailureKind uint8

const (
	ProtectionReserved ProtectionFailureKind = iota + 1
	ProtectionUnverified
)

type ProtectionUncertainty uint8

const (
	ProtectionLocationUnavailable ProtectionUncertainty = iota + 1
	ProtectionEndpointUnavailable
	ProtectionDocumentUnavailable
	ProtectionDocumentIndeterminate
	ProtectionRegionIndeterminate
	ProtectionSelectorMissing
	ProtectionAllocationUnavailable
	ProtectionWorkLimit
)

// NativeProtectionFailure exposes safe contract facts without coupling a
// diagnostic boundary to filesystem-effect implementation packages.
type NativeProtectionFailure interface {
	error
	Kind() ProtectionFailureKind
	Uncertainty() ProtectionUncertainty
	Address() aggregate.DocumentAddress
	Host() target.Target
	Reference() output.RootReference
	Source() string
	Endpoint() string
	Reservation() PathReservation
}

type NativeClaimRefusalKind uint8

const (
	NativeClaimProjectGlobal NativeClaimRefusalKind = iota + 1
	NativeClaimPhysicalRole
	NativeClaimScopeTransfer
)

// NativeClaimFailure is a new-intent refusal, not execution or recovery status.
type NativeClaimFailure interface {
	error
	Kind() NativeClaimRefusalKind
	Address() aggregate.DocumentAddress
	GlobalLocation() (output.RootReference, bool)
}
