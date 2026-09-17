package durable

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

// TransferAuthority changes only the owner of retained carrier facts. It does
// not promote pending evidence or refresh historical observations.
func (snapshot Snapshot) TransferAuthority(from, to stateauthority.Authority) (Snapshot, error) {
	if err := from.Validate(); err != nil {
		return Snapshot{}, err
	}
	if err := to.Validate(); err != nil {
		return Snapshot{}, err
	}
	input := snapshot.input()
	for index, claim := range input.ManagedCarrierClaims {
		if !claim.Owner().Equal(from) {
			return Snapshot{}, fmt.Errorf("managed carrier claim has foreign state authority")
		}
		owner, err := claim.Owner().WithStatefile(to.StatefileAuthority())
		if err != nil {
			return Snapshot{}, err
		}
		replacement, err := claim.WithOwner(owner)
		if err != nil {
			return Snapshot{}, err
		}
		input.ManagedCarrierClaims[index] = replacement
	}
	for index, pending := range input.PendingCarrierInstalls {
		if !pending.Owner().Equal(from) {
			return Snapshot{}, fmt.Errorf("pending carrier install has foreign state authority")
		}
		owner, err := pending.Owner().WithStatefile(to.StatefileAuthority())
		if err != nil {
			return Snapshot{}, err
		}
		replacement, err := carrier.NewPendingCarrierInstall(owner, pending.Identity(), pending.InstallRequest())
		if err != nil {
			return Snapshot{}, err
		}
		input.PendingCarrierInstalls[index] = replacement
	}
	for index, pending := range input.PendingCarrierRemovals {
		if !pending.Owner().Equal(from) {
			return Snapshot{}, fmt.Errorf("pending carrier removal has foreign state authority")
		}
		claim := pending.Claim()
		owner, err := claim.Owner().WithStatefile(to.StatefileAuthority())
		if err != nil {
			return Snapshot{}, err
		}
		replacement, err := claim.WithOwner(owner)
		if err != nil {
			return Snapshot{}, err
		}
		removal, err := carrier.NewPendingCarrierRemoval(replacement, pending.RemoveRequest(), pending.EffectPostconditions(), pending.EffectBaselines())
		if err != nil {
			return Snapshot{}, err
		}
		input.PendingCarrierRemovals[index] = removal
	}
	return NewSnapshot(input)
}
