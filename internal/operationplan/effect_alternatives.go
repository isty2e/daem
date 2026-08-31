package operationplan

import (
	"fmt"
	"sort"
)

const (
	maximumEffectDemandAlternatives = 4096
	maximumEffectTriggerRelations   = 64
	maximumEffectRepeatIterations   = 4096
)

type effectDemandAlternative struct {
	demand       effectDemand
	triggered    uint64
	conditionals uint64
}

// DemandAlternatives returns the deterministic nondominated frontier of
// cursor-reachable semantic demand alternatives. Dominance is valid only for
// State Barrier lowering that is monotone in every demand dimension. Returned
// demands carry no descendant path binding; the barrier supplies that boundary
// fact before taking the physical maximum.
func (structure EffectStructure) DemandAlternatives() ([]Demand, error) {
	triggerBits, err := effectTriggerBits(structure.root)
	if err != nil {
		return nil, err
	}
	alternatives, err := advanceEffectDemandAlternatives(
		structure.root,
		[]effectDemandAlternative{{}},
		triggerBits,
	)
	if err != nil {
		return nil, err
	}

	reachable := make([]effectDemandAlternative, 0, len(alternatives))
	for _, alternative := range alternatives {
		if alternative.demand.forwardEffectCalls != 0 {
			return nil, fmt.Errorf("operationplan: unlowered forward effect demand remains")
		}
		if alternative.triggered&^alternative.conditionals != 0 {
			continue
		}
		alternative.triggered = 0
		alternative.conditionals = 0
		reachable = append(reachable, alternative)
	}
	if len(reachable) == 0 {
		return nil, fmt.Errorf("operationplan: effect structure has no cursor-reachable demand alternative")
	}
	reachable, err = pruneEffectDemandAlternatives(reachable)
	if err != nil {
		return nil, err
	}
	sort.Slice(reachable, func(left int, right int) bool {
		return effectDemandLess(reachable[left].demand, reachable[right].demand)
	})
	result := make([]Demand, len(reachable))
	for index, alternative := range reachable {
		result[index] = Demand{
			ensureCalls:             alternative.demand.ensureCalls,
			barrierValidationCalls:  alternative.demand.barrierValidationCalls,
			stateDirValidationCalls: alternative.demand.stateDirValidationCalls,
			descendantValidations:   alternative.demand.descendantValidations,
			descendantFileCommits:   alternative.demand.descendantFileCommits,
		}
	}
	return result, nil
}

func advanceEffectDemandAlternatives(
	node EffectNode,
	input []effectDemandAlternative,
	triggerBits map[string]uint,
) ([]effectDemandAlternative, error) {
	switch node.kind {
	case effectNodeEmpty:
		return cloneEffectDemandAlternatives(input), nil
	case effectNodeStep:
		delta := effectStepDemand(node.step.kind)
		result := cloneEffectDemandAlternatives(input)
		for index := range result {
			var err error
			result[index].demand, err = result[index].demand.add(delta)
			if err != nil {
				return nil, err
			}
		}
		return pruneEffectDemandAlternatives(result)
	case effectNodeSequence:
		result := cloneEffectDemandAlternatives(input)
		for _, child := range node.children {
			var err error
			result, err = advanceEffectDemandAlternatives(
				child,
				result,
				triggerBits,
			)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	case effectNodeChoice:
		var result []effectDemandAlternative
		for _, child := range node.children {
			childAlternatives, err := advanceEffectDemandAlternatives(
				child,
				input,
				triggerBits,
			)
			if err != nil {
				return nil, err
			}
			result = append(result, childAlternatives...)
			result, err = pruneEffectDemandAlternatives(result)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	case effectNodeRepeat:
		if deterministicEffectNode(node.children[0]) {
			delta, err := legacyUpperBoundDemand(node.children[0])
			if err != nil {
				return nil, err
			}
			delta, err = delta.multiply(node.repetitions)
			if err != nil {
				return nil, err
			}
			result := cloneEffectDemandAlternatives(input)
			for index := range result {
				result[index].demand, err = result[index].demand.add(delta)
				if err != nil {
					return nil, err
				}
			}
			return pruneEffectDemandAlternatives(result)
		}
		if node.repetitions > maximumEffectRepeatIterations {
			return nil, fmt.Errorf(
				"operationplan: nondeterministic effect repetition %d exceeds lowering limit %d",
				node.repetitions,
				maximumEffectRepeatIterations,
			)
		}
		result := cloneEffectDemandAlternatives(input)
		for range node.repetitions {
			var err error
			result, err = advanceEffectDemandAlternatives(
				node.children[0],
				result,
				triggerBits,
			)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	case effectNodeForwardPhase:
		result, err := advanceEffectDemandAlternatives(
			node.children[0],
			input,
			triggerBits,
		)
		if err != nil {
			return nil, err
		}
		for index := range result {
			forwardCalls := result[index].demand.forwardEffectCalls
			if forwardCalls == 0 {
				continue
			}
			result[index].demand.forwardEffectCalls = 0
			result[index].demand.ensureCalls, err = checkedAdd(
				result[index].demand.ensureCalls,
				1,
			)
			if err != nil {
				return nil, err
			}
			result[index].demand.stateDirValidationCalls, err = checkedAdd(
				result[index].demand.stateDirValidationCalls,
				forwardCalls-1,
			)
			if err != nil {
				return nil, err
			}
		}
		return pruneEffectDemandAlternatives(result)
	case effectNodeTrigger:
		bit := uint64(1) << triggerBits[node.triggerID]
		triggered := make([]effectDemandAlternative, 0, len(input))
		for _, alternative := range input {
			if alternative.conditionals&bit != 0 {
				continue
			}
			alternative.triggered |= bit
			triggered = append(triggered, alternative)
		}
		if len(triggered) == 0 {
			return nil, nil
		}
		return advanceEffectDemandAlternatives(
			node.children[0],
			triggered,
			triggerBits,
		)
	case effectNodeConditional:
		bit := uint64(1) << triggerBits[node.triggerID]
		var result []effectDemandAlternative
		for _, alternative := range input {
			alternative.conditionals |= bit
			if alternative.triggered&bit == 0 {
				result = append(result, alternative)
				continue
			}
			childAlternatives, err := advanceEffectDemandAlternatives(
				node.children[0],
				[]effectDemandAlternative{alternative},
				triggerBits,
			)
			if err != nil {
				return nil, err
			}
			result = append(result, childAlternatives...)
		}
		return pruneEffectDemandAlternatives(result)
	default:
		return nil, fmt.Errorf(
			"operationplan: effect structure has invalid node kind %d",
			node.kind,
		)
	}
}

func effectTriggerBits(root EffectNode) (map[string]uint, error) {
	ids := make(map[string]struct{})
	collectEffectTriggerIDs(root, ids)
	if len(ids) > maximumEffectTriggerRelations {
		return nil, fmt.Errorf(
			"operationplan: effect trigger relation count %d exceeds lowering limit %d",
			len(ids),
			maximumEffectTriggerRelations,
		)
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	result := make(map[string]uint, len(ordered))
	for index, id := range ordered {
		result[id] = uint(index)
	}
	return result, nil
}

func collectEffectTriggerIDs(node EffectNode, result map[string]struct{}) {
	if node.kind == effectNodeTrigger || node.kind == effectNodeConditional {
		result[node.triggerID] = struct{}{}
	}
	for _, child := range node.children {
		collectEffectTriggerIDs(child, result)
	}
}

func deterministicEffectNode(node EffectNode) bool {
	switch node.kind {
	case effectNodeChoice, effectNodeTrigger, effectNodeConditional:
		return false
	}
	for _, child := range node.children {
		if !deterministicEffectNode(child) {
			return false
		}
	}
	return true
}

func pruneEffectDemandAlternatives(
	alternatives []effectDemandAlternative,
) ([]effectDemandAlternative, error) {
	if len(alternatives) == 0 {
		return nil, nil
	}
	result := make([]effectDemandAlternative, 0, min(len(alternatives), maximumEffectDemandAlternatives))
	for _, candidate := range alternatives {
		dominated := false
		for index := 0; index < len(result); {
			existing := result[index]
			if candidate.triggered != existing.triggered ||
				candidate.conditionals != existing.conditionals {
				index++
				continue
			}
			switch {
			case effectDemandDominates(existing.demand, candidate.demand):
				dominated = true
				index = len(result)
			case effectDemandDominates(candidate.demand, existing.demand):
				result = append(result[:index], result[index+1:]...)
			default:
				index++
			}
		}
		if dominated {
			continue
		}
		result = append(result, candidate)
		if len(result) > maximumEffectDemandAlternatives {
			return nil, fmt.Errorf(
				"operationplan: effect demand alternatives exceed lowering limit %d",
				maximumEffectDemandAlternatives,
			)
		}
	}
	return result, nil
}

func effectDemandDominates(left effectDemand, right effectDemand) bool {
	return left.forwardEffectCalls >= right.forwardEffectCalls &&
		left.ensureCalls >= right.ensureCalls &&
		left.barrierValidationCalls >= right.barrierValidationCalls &&
		left.stateDirValidationCalls >= right.stateDirValidationCalls &&
		left.descendantValidations >= right.descendantValidations &&
		left.descendantFileCommits >= right.descendantFileCommits
}

func effectDemandLess(left effectDemand, right effectDemand) bool {
	leftValues := [...]int{
		left.ensureCalls,
		left.barrierValidationCalls,
		left.stateDirValidationCalls,
		left.descendantValidations,
		left.descendantFileCommits,
	}
	rightValues := [...]int{
		right.ensureCalls,
		right.barrierValidationCalls,
		right.stateDirValidationCalls,
		right.descendantValidations,
		right.descendantFileCommits,
	}
	for index := range leftValues {
		if leftValues[index] != rightValues[index] {
			return leftValues[index] < rightValues[index]
		}
	}
	return false
}

func cloneEffectDemandAlternatives(
	alternatives []effectDemandAlternative,
) []effectDemandAlternative {
	return append([]effectDemandAlternative(nil), alternatives...)
}
