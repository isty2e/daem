// Package applyprogram owns the compiled Apply program and its exact forward,
// failure-settlement and visibility cursor. It grants no mutation authority.
package applyprogram

import (
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
)

type Plan struct {
	segment          operationplan.EffectNode
	structure        operationplan.EffectStructure
	failureStructure operationplan.EffectStructure
	promotionIndexes map[ownership.LogicalKey]int
	changed          bool
	valid            bool
}

func NoOpPlan() (Plan, error) {
	var builder operationplan.EffectStructureBuilder
	segment := operationplan.EffectSequence()
	structure, err := builder.Compile(segment)
	if err != nil {
		return Plan{}, err
	}
	return newPlan(segment, structure, operationplan.EffectStructure{}, nil, false)
}

func newPlan(segment operationplan.EffectNode, structure, failure operationplan.EffectStructure, promotions map[ownership.LogicalKey]int, changed bool) (Plan, error) {
	if changed {
		if err := validateFailurePolicy(failure); err != nil {
			return Plan{}, err
		}
	}
	return Plan{
		segment: segment, structure: structure, failureStructure: failure,
		promotionIndexes: clonePromotionIndexes(promotions), changed: changed, valid: true,
	}, nil
}

func (plan Plan) Segment() operationplan.EffectNode { return plan.segment }

func (plan Plan) SameStructure(other Plan) bool {
	return plan.structure.Equal(other.structure)
}

func (plan Plan) SameFailurePolicy(other Plan) bool {
	return plan.failureStructure.Equal(other.failureStructure)
}
