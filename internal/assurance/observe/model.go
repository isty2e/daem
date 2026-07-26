package observe

import (
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

// OwnershipObservation reports the durable claim, if any, overlapping one resolved output address.
type OwnershipObservation struct {
	Destination output.Destination
	ContentPath output.ContentPath
	Address     ownership.ManagedAddress
	Claim       ownership.ClaimValue
}
