package ownership

// ClaimValue is an explicit optional claim used by registry comparisons and
// ownership-mutation protocols.
type ClaimValue struct {
	present bool
	claim   Claim
}

// NoClaim returns an explicit absent claim value.
func NoClaim() ClaimValue {
	return ClaimValue{}
}

// PresentClaim validates and wraps a present claim value.
func PresentClaim(claim Claim) (ClaimValue, error) {
	if err := claim.Validate(); err != nil {
		return ClaimValue{}, err
	}
	return ClaimValue{present: true, claim: claim}, nil
}

// Get returns the claim and whether it is present.
func (value ClaimValue) Get() (Claim, bool) {
	return value.claim, value.present
}

// Equal reports exact optional-claim equality.
func (value ClaimValue) Equal(other ClaimValue) bool {
	if value.present != other.present {
		return false
	}
	return !value.present || value.claim.Equal(other.claim)
}
