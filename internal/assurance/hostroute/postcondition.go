package hostroute

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/effectpostcondition"
)

// PostconditionRequirement composes the primary host-relation postcondition
// with the exact route-coupled effect facts selected by the locked operation.
type PostconditionRequirement struct {
	present  bool
	relation RelationPostcondition
	effects  effectpostcondition.Set
}

// RequireRelationPostcondition constructs an explicit relation-only
// postcondition contract.
func RequireRelationPostcondition(
	relation RelationPostcondition,
) PostconditionRequirement {
	return PostconditionRequirement{
		present:  true,
		relation: relation,
	}
}

// RequirePostconditions constructs an explicit composite postcondition
// contract. A non-empty locked effect set cannot be silently downgraded by a
// missing observation.
func RequirePostconditions(
	relation RelationPostcondition,
	effects effectpostcondition.Set,
) PostconditionRequirement {
	return PostconditionRequirement{
		present:  true,
		relation: relation,
		effects:  effects,
	}
}

func (requirement PostconditionRequirement) validate() error {
	if !requirement.present {
		return fmt.Errorf("postcondition requirement is required")
	}
	if err := requirement.relation.validate(); err != nil {
		return err
	}
	if err := requirement.effects.Validate(); err != nil {
		return err
	}
	return nil
}

func (requirement PostconditionRequirement) relationPostcondition() RelationPostcondition {
	return requirement.relation
}

func (requirement PostconditionRequirement) effectPostconditions() effectpostcondition.Set {
	return requirement.effects
}
