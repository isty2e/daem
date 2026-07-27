package carrieradoption

import (
	"cmp"
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func relevantCarrierAdoptionClaims(
	owner stateauthority.Authority,
	identity durablecarrier.ManagedCarrierIdentity,
	claims []durablecarrier.ManagedCarrierClaim,
) []durablecarrier.ManagedCarrierClaim {
	relevant := make([]durablecarrier.ManagedCarrierClaim, 0, len(claims))
	for _, claim := range claims {
		claimIdentity := claim.Identity()
		if claimIdentity.Carrier() == identity.Carrier() ||
			(claim.Owner().Equal(owner) &&
				claimIdentity.RelationSubject() == identity.RelationSubject()) ||
			(claimIdentity.Target() == identity.Target() &&
				claimIdentity.Scope() == identity.Scope() &&
				claimIdentity.ExpectedRelation().SubjectKey() ==
					identity.ExpectedRelation().SubjectKey()) {
			relevant = append(relevant, claim)
		}
	}
	return relevant
}

type claimAssessment struct {
	current    durablecarrier.ManagedCarrierClaim
	hasCurrent bool
	conflicts  []durablecarrier.ManagedCarrierClaim
}

func assessClaims(
	owner stateauthority.Authority,
	identity durablecarrier.ManagedCarrierIdentity,
	locked lock.LockedSubjectContract,
	claims []durablecarrier.ManagedCarrierClaim,
) claimAssessment {
	assessment := claimAssessment{}
	for _, claim := range claims {
		claimIdentity := claim.Identity()
		sameOwnerRelation := claim.Owner().Equal(owner) &&
			claimIdentity.RelationSubject() == identity.RelationSubject()
		if sameOwnerRelation {
			if claim.MatchesLockedRecord(locked) {
				assessment.current = claim
				assessment.hasCurrent = true
			} else {
				assessment.conflicts = append(assessment.conflicts, claim)
			}
			continue
		}
		if claimIdentity.Target() != identity.Target() ||
			claimIdentity.Scope() != identity.Scope() ||
			claimIdentity.ExpectedRelation().SubjectKey() != identity.ExpectedRelation().SubjectKey() {
			continue
		}
		if claimIdentity.Carrier() != identity.Carrier() ||
			(identity.Scope() == target.ScopeProject && !claim.Owner().Equal(owner)) {
			assessment.conflicts = append(assessment.conflicts, claim)
		}
	}
	return assessment
}

func canonicalClaims(values []durablecarrier.ManagedCarrierClaim) ([]durablecarrier.ManagedCarrierClaim, error) {
	claims := append([]durablecarrier.ManagedCarrierClaim(nil), values...)
	seen := make(map[string]struct{}, len(claims))
	for index, claim := range claims {
		if err := claim.Validate(); err != nil {
			return nil, fmt.Errorf("carrier adoption claim[%d]: %w", index, err)
		}
		key := claim.Owner().StatefileKey() + "\x00" +
			claim.Identity().RelationSubject().String()
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf(
				"carrier adoption claim[%d] duplicates one owner relation",
				index,
			)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(claims, func(left int, right int) bool {
		if order := cmp.Compare(
			claims[left].Owner().StatefileKey(),
			claims[right].Owner().StatefileKey(),
		); order != 0 {
			return order < 0
		}
		return topology.CompareSubjectID(
			claims[left].Identity().RelationSubject(),
			claims[right].Identity().RelationSubject(),
		) < 0
	})
	return claims, nil
}

func equalClaims(left []durablecarrier.ManagedCarrierClaim, right []durablecarrier.ManagedCarrierClaim) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].ExactEqual(right[index]) {
			return false
		}
	}
	return true
}

func equalOccupancy(left durablecarrier.CarrierOccupancy, right durablecarrier.CarrierOccupancy) bool {
	if left.Carrier() != right.Carrier() {
		return false
	}
	leftConsumers := left.DaemKnownConsumers()
	rightConsumers := right.DaemKnownConsumers()
	if len(leftConsumers) != len(rightConsumers) {
		return false
	}
	for index := range leftConsumers {
		if !leftConsumers[index].ExactEqual(rightConsumers[index]) {
			return false
		}
	}
	return true
}
