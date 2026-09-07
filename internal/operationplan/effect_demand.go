package operationplan

import "fmt"

type effectDemand struct {
	forwardEffectCalls      int
	ensureCalls             int
	barrierValidationCalls  int
	stateDirValidationCalls int
	descendantBindings      int
	descendantValidations   int
	descendantFileCommits   int
}

func (demand effectDemand) add(other effectDemand) (effectDemand, error) {
	var err error
	demand.forwardEffectCalls, err = checkedAdd(
		demand.forwardEffectCalls,
		other.forwardEffectCalls,
	)
	if err != nil {
		return effectDemand{}, err
	}
	demand.ensureCalls, err = checkedAdd(demand.ensureCalls, other.ensureCalls)
	if err != nil {
		return effectDemand{}, err
	}
	demand.barrierValidationCalls, err = checkedAdd(
		demand.barrierValidationCalls,
		other.barrierValidationCalls,
	)
	if err != nil {
		return effectDemand{}, err
	}
	demand.stateDirValidationCalls, err = checkedAdd(
		demand.stateDirValidationCalls,
		other.stateDirValidationCalls,
	)
	if err != nil {
		return effectDemand{}, err
	}
	demand.descendantBindings, err = checkedAdd(
		demand.descendantBindings,
		other.descendantBindings,
	)
	if err != nil {
		return effectDemand{}, err
	}
	demand.descendantValidations, err = checkedAdd(
		demand.descendantValidations,
		other.descendantValidations,
	)
	if err != nil {
		return effectDemand{}, err
	}
	demand.descendantFileCommits, err = checkedAdd(
		demand.descendantFileCommits,
		other.descendantFileCommits,
	)
	if err != nil {
		return effectDemand{}, err
	}
	return demand, nil
}

func (demand effectDemand) multiply(count int) (effectDemand, error) {
	var err error
	demand.forwardEffectCalls, err = checkedMul(demand.forwardEffectCalls, count)
	if err != nil {
		return effectDemand{}, err
	}
	demand.ensureCalls, err = checkedMul(demand.ensureCalls, count)
	if err != nil {
		return effectDemand{}, err
	}
	demand.barrierValidationCalls, err = checkedMul(demand.barrierValidationCalls, count)
	if err != nil {
		return effectDemand{}, err
	}
	demand.stateDirValidationCalls, err = checkedMul(demand.stateDirValidationCalls, count)
	if err != nil {
		return effectDemand{}, err
	}
	demand.descendantBindings, err = checkedMul(demand.descendantBindings, count)
	if err != nil {
		return effectDemand{}, err
	}
	demand.descendantValidations, err = checkedMul(demand.descendantValidations, count)
	if err != nil {
		return effectDemand{}, err
	}
	demand.descendantFileCommits, err = checkedMul(demand.descendantFileCommits, count)
	if err != nil {
		return effectDemand{}, err
	}
	return demand, nil
}

func (demand effectDemand) maximum(other effectDemand) effectDemand {
	return effectDemand{
		forwardEffectCalls:      max(demand.forwardEffectCalls, other.forwardEffectCalls),
		ensureCalls:             max(demand.ensureCalls, other.ensureCalls),
		barrierValidationCalls:  max(demand.barrierValidationCalls, other.barrierValidationCalls),
		stateDirValidationCalls: max(demand.stateDirValidationCalls, other.stateDirValidationCalls),
		descendantBindings:      max(demand.descendantBindings, other.descendantBindings),
		descendantValidations:   max(demand.descendantValidations, other.descendantValidations),
		descendantFileCommits:   max(demand.descendantFileCommits, other.descendantFileCommits),
	}
}

// legacyUpperBound reproduces the flat counter projection used by the current
// reservation seed. It is a shadow projection rather than structure validity:
// collapsing a choice before physical lowering can combine dimensions that no
// reachable alternative consumes together.
func (structure EffectStructure) legacyUpperBound() (effectDemand, error) {
	return legacyUpperBoundDemand(structure.root)
}

// LegacyDemand projects the current flat reservation counters for differential
// migration tests. It intentionally ignores terminal control, so later suffixes
// and repetitions remain part of the conservative legacy bound.
func (structure EffectStructure) LegacyDemand() (Demand, error) {
	legacy, err := structure.legacyUpperBound()
	if err != nil {
		return Demand{}, err
	}
	if legacy.forwardEffectCalls != 0 {
		return Demand{}, fmt.Errorf("operationplan: unlowered forward effect demand remains")
	}
	return Demand{
		ensureCalls:             legacy.ensureCalls,
		barrierValidationCalls:  legacy.barrierValidationCalls,
		stateDirValidationCalls: legacy.stateDirValidationCalls,
		descendantBindings:      legacy.descendantBindings,
		descendantValidations:   legacy.descendantValidations,
		descendantFileCommits:   legacy.descendantFileCommits,
	}, nil
}

func legacyUpperBoundDemand(node EffectNode) (effectDemand, error) {
	switch node.kind {
	case effectNodeEmpty:
		return effectDemand{}, nil
	case effectNodeStep:
		return effectStepDemand(node.step.kind), nil
	case effectNodeSequence:
		var demand effectDemand
		for _, child := range node.children {
			childDemand, err := legacyUpperBoundDemand(child)
			if err != nil {
				return effectDemand{}, err
			}
			demand, err = demand.add(childDemand)
			if err != nil {
				return effectDemand{}, err
			}
		}
		return demand, nil
	case effectNodeChoice:
		var demand effectDemand
		for _, child := range node.children {
			childDemand, err := legacyUpperBoundDemand(child)
			if err != nil {
				return effectDemand{}, err
			}
			demand = demand.maximum(childDemand)
		}
		return demand, nil
	case effectNodeRepeat:
		childDemand, err := legacyUpperBoundDemand(node.children[0])
		if err != nil {
			return effectDemand{}, err
		}
		return childDemand.multiply(node.repetitions)
	case effectNodeForwardPhase:
		childDemand, err := legacyUpperBoundDemand(node.children[0])
		if err != nil {
			return effectDemand{}, err
		}
		if childDemand.forwardEffectCalls == 0 {
			return childDemand, nil
		}
		childDemand.ensureCalls, err = checkedAdd(childDemand.ensureCalls, 1)
		if err != nil {
			return effectDemand{}, err
		}
		childDemand.stateDirValidationCalls, err = checkedAdd(
			childDemand.stateDirValidationCalls,
			childDemand.forwardEffectCalls-1,
		)
		if err != nil {
			return effectDemand{}, err
		}
		childDemand.forwardEffectCalls = 0
		return childDemand, nil
	case effectNodeTrigger, effectNodeConditional:
		return legacyUpperBoundDemand(node.children[0])
	default:
		return effectDemand{}, fmt.Errorf(
			"operationplan: effect structure has invalid node kind %d",
			node.kind,
		)
	}
}

func effectStepDemand(kind EffectStepKind) effectDemand {
	switch kind {
	case EffectStepEstablishStateDir:
		return effectDemand{ensureCalls: 1}
	case EffectStepValidateBarrier:
		return effectDemand{barrierValidationCalls: 1}
	case EffectStepValidateStateDir:
		return effectDemand{stateDirValidationCalls: 1}
	case EffectStepForwardEffect:
		return effectDemand{forwardEffectCalls: 1}
	case EffectStepBindDescendant:
		return effectDemand{descendantBindings: 1}
	case EffectStepValidateDescendant:
		return effectDemand{descendantValidations: 1}
	case EffectStepPublishDescendant:
		return effectDemand{descendantFileCommits: 1}
	default:
		return effectDemand{}
	}
}
