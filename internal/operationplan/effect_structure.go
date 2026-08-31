package operationplan

import (
	"fmt"
	"strings"
)

type effectStepKind uint8

const (
	effectStepEstablishStateDir effectStepKind = iota + 1
	effectStepValidateBarrier
	effectStepValidateStateDir
	effectStepValidateDescendant
	effectStepPublishDescendant
	effectStepExternal
	effectStepObservation
	effectStepPersistence
	effectStepCompensation
	effectStepCleanup
	effectStepRetirement
	effectStepTerminal
)

type effectNodeKind uint8

const (
	effectNodeEmpty effectNodeKind = iota
	effectNodeStep
	effectNodeSequence
	effectNodeChoice
	effectNodeRepeat
)

type effectStep struct {
	id   string
	kind effectStepKind
}

type effectNode struct {
	kind        effectNodeKind
	step        effectStep
	children    []effectNode
	choiceID    string
	repetitions int
}

type effectDemand struct {
	ensureCalls             int
	barrierValidationCalls  int
	stateDirValidationCalls int
	descendantValidations   int
	descendantFileCommits   int
}

func (demand effectDemand) add(other effectDemand) (effectDemand, error) {
	var err error
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
		ensureCalls:             max(demand.ensureCalls, other.ensureCalls),
		barrierValidationCalls:  max(demand.barrierValidationCalls, other.barrierValidationCalls),
		stateDirValidationCalls: max(demand.stateDirValidationCalls, other.stateDirValidationCalls),
		descendantValidations:   max(demand.descendantValidations, other.descendantValidations),
		descendantFileCommits:   max(demand.descendantFileCommits, other.descendantFileCommits),
	}
}

type effectStructure struct {
	root effectNode
}

func newEffectStep(id string, kind effectStepKind) (effectNode, error) {
	if err := validateEffectReference("step", id); err != nil {
		return effectNode{}, err
	}
	if !kind.valid() {
		return effectNode{}, fmt.Errorf("operationplan: effect step %q has invalid kind %d", id, kind)
	}
	return effectNode{kind: effectNodeStep, step: effectStep{id: id, kind: kind}}, nil
}

func newEffectSequence(children ...effectNode) effectNode {
	if len(children) == 0 {
		return effectNode{kind: effectNodeEmpty}
	}
	return effectNode{kind: effectNodeSequence, children: cloneEffectNodes(children)}
}

func newEffectChoice(id string, alternatives ...effectNode) (effectNode, error) {
	if err := validateEffectReference("choice", id); err != nil {
		return effectNode{}, err
	}
	if len(alternatives) < 2 {
		return effectNode{}, fmt.Errorf("operationplan: effect choice %q requires at least two alternatives", id)
	}
	return effectNode{
		kind:     effectNodeChoice,
		children: cloneEffectNodes(alternatives),
		choiceID: id,
	}, nil
}

func newEffectRepeat(count int, body effectNode) (effectNode, error) {
	if count < 0 {
		return effectNode{}, fmt.Errorf("operationplan: effect repetition must not be negative")
	}
	if count == 0 {
		return effectNode{kind: effectNodeEmpty}, nil
	}
	return effectNode{
		kind:        effectNodeRepeat,
		children:    []effectNode{cloneEffectNode(body)},
		repetitions: count,
	}, nil
}

func compileEffectStructure(root effectNode) (effectStructure, error) {
	canonical := cloneEffectNode(root)
	stepIDs := make(map[string]struct{})
	choiceIDs := make(map[string]struct{})
	if err := validateEffectNode(canonical, stepIDs, choiceIDs); err != nil {
		return effectStructure{}, err
	}
	return effectStructure{root: canonical}, nil
}

func validateEffectNode(
	node effectNode,
	stepIDs map[string]struct{},
	choiceIDs map[string]struct{},
) error {
	switch node.kind {
	case effectNodeEmpty:
		return nil
	case effectNodeStep:
		if err := validateEffectReference("step", node.step.id); err != nil {
			return err
		}
		if !node.step.kind.valid() {
			return fmt.Errorf(
				"operationplan: effect step %q has invalid kind %d",
				node.step.id,
				node.step.kind,
			)
		}
		if _, duplicate := stepIDs[node.step.id]; duplicate {
			return fmt.Errorf("operationplan: duplicate effect step %q", node.step.id)
		}
		stepIDs[node.step.id] = struct{}{}
		return nil
	case effectNodeSequence:
		if len(node.children) == 0 {
			return fmt.Errorf("operationplan: effect sequence is empty")
		}
		for _, child := range node.children {
			if err := validateEffectNode(child, stepIDs, choiceIDs); err != nil {
				return err
			}
		}
		return nil
	case effectNodeChoice:
		if err := validateEffectReference("choice", node.choiceID); err != nil {
			return err
		}
		if len(node.children) < 2 {
			return fmt.Errorf(
				"operationplan: effect choice %q requires at least two alternatives",
				node.choiceID,
			)
		}
		if _, duplicate := choiceIDs[node.choiceID]; duplicate {
			return fmt.Errorf("operationplan: duplicate effect choice %q", node.choiceID)
		}
		choiceIDs[node.choiceID] = struct{}{}
		for _, child := range node.children {
			if err := validateEffectNode(child, stepIDs, choiceIDs); err != nil {
				return err
			}
		}
		return nil
	case effectNodeRepeat:
		if node.repetitions <= 0 || len(node.children) != 1 {
			return fmt.Errorf("operationplan: effect repetition is invalid")
		}
		return validateEffectNode(node.children[0], stepIDs, choiceIDs)
	default:
		return fmt.Errorf("operationplan: effect structure has invalid node kind %d", node.kind)
	}
}

// legacyUpperBound reproduces the flat counter projection used by the current
// reservation seed. It is a shadow projection rather than structure validity:
// collapsing a choice before physical lowering can combine dimensions that no
// reachable alternative consumes together.
func (structure effectStructure) legacyUpperBound() (effectDemand, error) {
	return legacyUpperBoundDemand(structure.root)
}

func legacyUpperBoundDemand(node effectNode) (effectDemand, error) {
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
	default:
		return effectDemand{}, fmt.Errorf(
			"operationplan: effect structure has invalid node kind %d",
			node.kind,
		)
	}
}

func effectStepDemand(kind effectStepKind) effectDemand {
	switch kind {
	case effectStepEstablishStateDir:
		return effectDemand{ensureCalls: 1}
	case effectStepValidateBarrier:
		return effectDemand{barrierValidationCalls: 1}
	case effectStepValidateStateDir:
		return effectDemand{stateDirValidationCalls: 1}
	case effectStepValidateDescendant:
		return effectDemand{descendantValidations: 1}
	case effectStepPublishDescendant:
		return effectDemand{descendantFileCommits: 1}
	default:
		return effectDemand{}
	}
}

func (kind effectStepKind) valid() bool {
	return kind >= effectStepEstablishStateDir && kind <= effectStepTerminal
}

func (kind effectStepKind) startsEffect() bool {
	switch kind {
	case effectStepEstablishStateDir,
		effectStepPublishDescendant,
		effectStepExternal,
		effectStepPersistence,
		effectStepCompensation,
		effectStepCleanup,
		effectStepRetirement:
		return true
	default:
		return false
	}
}

func validateEffectReference(label string, value string) error {
	canonical := strings.TrimSpace(value)
	if canonical == "" {
		return fmt.Errorf("operationplan: effect %s reference is empty", label)
	}
	if canonical != value {
		return fmt.Errorf("operationplan: effect %s reference is not canonical", label)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("operationplan: effect %s reference contains NUL", label)
	}
	return nil
}

func cloneEffectNodes(nodes []effectNode) []effectNode {
	result := make([]effectNode, len(nodes))
	for index, node := range nodes {
		result[index] = cloneEffectNode(node)
	}
	return result
}

func cloneEffectNode(node effectNode) effectNode {
	node.children = cloneEffectNodes(node.children)
	return node
}

type effectCursorState uint8

const (
	effectCursorActive effectCursorState = iota
	effectCursorFinished
	effectCursorAborted
)

type effectCursorFrame struct {
	node     *effectNode
	next     int
	selected int
}

type effectCursor struct {
	root          effectNode
	stack         []effectCursorFrame
	state         effectCursorState
	effectStarted bool
}

func newEffectCursor(structure effectStructure) *effectCursor {
	root := cloneEffectNode(structure.root)
	cursor := &effectCursor{root: root}
	cursor.push(&cursor.root)
	return cursor
}

func (cursor *effectCursor) selectAlternative(choiceID string, alternative int) error {
	if err := cursor.requireActive(); err != nil {
		return err
	}
	step, choice, done, err := cursor.next()
	if err != nil {
		return err
	}
	if done || step != nil || choice == nil {
		return fmt.Errorf("operationplan: no effect choice is pending")
	}
	if choice.node.choiceID != choiceID {
		return fmt.Errorf(
			"operationplan: pending effect choice is %q, not %q",
			choice.node.choiceID,
			choiceID,
		)
	}
	if alternative < 0 || alternative >= len(choice.node.children) {
		return fmt.Errorf(
			"operationplan: effect choice %q alternative %d is out of range",
			choiceID,
			alternative,
		)
	}
	choice.selected = alternative
	return nil
}

func (cursor *effectCursor) consume(stepID string, kind effectStepKind) error {
	if err := cursor.requireActive(); err != nil {
		return err
	}
	step, choice, done, err := cursor.next()
	if err != nil {
		return err
	}
	if done {
		return fmt.Errorf("operationplan: effect structure has no remaining step")
	}
	if choice != nil {
		return fmt.Errorf(
			"operationplan: effect choice %q must be selected before consumption",
			choice.node.choiceID,
		)
	}
	if step == nil {
		return fmt.Errorf("operationplan: effect cursor has no consumable step")
	}
	if step.node.step.id != stepID || step.node.step.kind != kind {
		return fmt.Errorf(
			"operationplan: next effect step is %q/%d, not %q/%d",
			step.node.step.id,
			step.node.step.kind,
			stepID,
			kind,
		)
	}
	if kind.startsEffect() {
		cursor.effectStarted = true
	}
	cursor.pop()
	return nil
}

func (cursor *effectCursor) finishSuccess() error {
	if err := cursor.requireActive(); err != nil {
		return err
	}
	step, choice, done, err := cursor.next()
	if err != nil {
		return err
	}
	if !done {
		if choice != nil {
			return fmt.Errorf(
				"operationplan: effect choice %q remains unselected",
				choice.node.choiceID,
			)
		}
		if step == nil {
			return fmt.Errorf("operationplan: effect cursor has no consumable step")
		}
		return fmt.Errorf(
			"operationplan: mandatory effect step %q/%d was not consumed",
			step.node.step.id,
			step.node.step.kind,
		)
	}
	cursor.state = effectCursorFinished
	return nil
}

func (cursor *effectCursor) abortBeforeEffect() error {
	if err := cursor.requireActive(); err != nil {
		return err
	}
	if cursor.effectStarted {
		return fmt.Errorf("operationplan: effect structure cannot abort before effect after an effect started")
	}
	cursor.stack = nil
	cursor.state = effectCursorAborted
	return nil
}

func (cursor *effectCursor) requireActive() error {
	if cursor == nil {
		return fmt.Errorf("operationplan: effect cursor is unavailable")
	}
	switch cursor.state {
	case effectCursorActive:
		return nil
	case effectCursorFinished:
		return fmt.Errorf("operationplan: effect cursor is already finished")
	case effectCursorAborted:
		return fmt.Errorf("operationplan: effect cursor is already aborted")
	default:
		return fmt.Errorf("operationplan: effect cursor has invalid state")
	}
}

func (cursor *effectCursor) next() (
	step *effectCursorFrame,
	choice *effectCursorFrame,
	done bool,
	err error,
) {
	for len(cursor.stack) != 0 {
		frame := &cursor.stack[len(cursor.stack)-1]
		switch frame.node.kind {
		case effectNodeEmpty:
			cursor.pop()
		case effectNodeStep:
			return frame, nil, false, nil
		case effectNodeSequence:
			if frame.next == len(frame.node.children) {
				cursor.pop()
				continue
			}
			child := &frame.node.children[frame.next]
			frame.next++
			cursor.push(child)
		case effectNodeChoice:
			if frame.selected < 0 {
				return nil, frame, false, nil
			}
			if frame.next != 0 {
				cursor.pop()
				continue
			}
			frame.next = 1
			cursor.push(&frame.node.children[frame.selected])
		case effectNodeRepeat:
			if frame.next == frame.node.repetitions {
				cursor.pop()
				continue
			}
			frame.next++
			cursor.push(&frame.node.children[0])
		default:
			return nil, nil, false, fmt.Errorf(
				"operationplan: effect cursor found invalid node kind %d",
				frame.node.kind,
			)
		}
	}
	return nil, nil, true, nil
}

func (cursor *effectCursor) push(node *effectNode) {
	cursor.stack = append(cursor.stack, effectCursorFrame{node: node, selected: -1})
}

func (cursor *effectCursor) pop() {
	cursor.stack = cursor.stack[:len(cursor.stack)-1]
}
