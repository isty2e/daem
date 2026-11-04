package ownership

import "fmt"

// Overlapping returns every actual claim overlapping address in canonical order.
// A matching first claim is not evidence that a whole request has no blockers.
func (registry Registry) Overlapping(address ManagedAddress) ([]Claim, error) {
	if !registry.initialized {
		return nil, fmt.Errorf("ownership registry is required")
	}
	if err := address.Validate(); err != nil {
		return nil, err
	}
	var result []Claim
	for _, claim := range registry.claims {
		if claim.Address().Overlaps(address) {
			result = append(result, claim)
		}
	}
	return result, nil
}
