package ownership

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

// TransferAuthority retains exact addresses and foreign owners. Outstanding
// reservations must be settled by their recovery operation before migration.
func (registry Registry) TransferAuthority(from, to stateauthority.Authority) (Registry, error) {
	if !registry.initialized {
		return Registry{}, fmt.Errorf("ownership registry is required")
	}
	if err := from.Validate(); err != nil {
		return Registry{}, err
	}
	if err := to.Validate(); err != nil {
		return Registry{}, err
	}
	next := registry.Claims()
	for index, claim := range next {
		if !claim.OwnedBy(from) {
			continue
		}
		if claim.State() != ClaimActive {
			return Registry{}, fmt.Errorf("reserved ownership claim requires recovery before migration")
		}
		owner, err := claim.Owner().WithStatefile(to.StatefileAuthority())
		if err != nil {
			return Registry{}, err
		}
		replacement, err := NewActiveClaim(claim.Address(), owner)
		if err != nil {
			return Registry{}, err
		}
		next[index] = replacement
	}
	return NewRegistry(next)
}
