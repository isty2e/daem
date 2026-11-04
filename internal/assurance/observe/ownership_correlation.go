package observe

// CorrelatesClaimAddress checks only claim/address correspondence. Missing
// history remains provisional; owner, reservation and local-state policy are
// separate from this comparison.
func (observation OwnershipObservation) CorrelatesClaimAddress() bool {
	if observation.Validate() != nil {
		return false
	}
	claim, present := observation.claim.Get()
	if !present {
		return false
	}
	if exact, current := observation.ExactAddress(); current {
		return claim.Address().Equal(exact)
	}
	return claim.Address().Footprint().Equal(observation.footprint) &&
		observation.provisional.MatchesMissingExact(claim.Address().PathAuthority())
}
