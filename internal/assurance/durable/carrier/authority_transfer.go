package carrier

import "github.com/isty2e/daem/internal/assurance/stateauthority"

// TransferAuthority preserves every relation and acquisition fact while moving
// only claims belonging to the exact source authority.
func (registry GlobalCarrierClaims) TransferAuthority(from, to stateauthority.Authority) (GlobalCarrierClaims, error) {
	if err := from.Validate(); err != nil {
		return GlobalCarrierClaims{}, err
	}
	if err := to.Validate(); err != nil {
		return GlobalCarrierClaims{}, err
	}
	next := registry.Claims()
	for index, claim := range next {
		if !claim.Owner().Equal(from) {
			continue
		}
		owner, err := claim.Owner().WithStatefile(to.StatefileAuthority())
		if err != nil {
			return GlobalCarrierClaims{}, err
		}
		replacement, err := NewManagedCarrierClaim(owner, claim.Identity(), claim.InstallRequest(), claim.Provenance())
		if err != nil {
			return GlobalCarrierClaims{}, err
		}
		next[index] = replacement
	}
	return NewGlobalCarrierClaims(next)
}
