package operationplan

import "fmt"

type effectCursorState uint8

const (
	effectCursorActive effectCursorState = iota
	effectCursorFinished
	effectCursorAborted
)

// ForwardEffectCheckpoint is the StateDir checkpoint implied by one selected
// forward effect inside its non-nested phase.
type ForwardEffectCheckpoint uint8

const (
	ForwardEffectEstablishStateDir ForwardEffectCheckpoint = iota + 1
	ForwardEffectValidateStateDir
)

type effectCursorFrame struct {
	node           *EffectNode
	next           int
	selected       int
	forwardEffects int
}

// EffectCursor validates branch selection and ordered runtime step consumption.
// It does not interpret effect outcomes or select rollback and durable successors.
type EffectCursor struct {
	root          EffectNode
	stack         []effectCursorFrame
	triggers      map[string]bool
	conditionals  map[string]bool
	state         effectCursorState
	effectStarted bool
	terminated    bool
}

// Begin returns one independent operation-local cursor over the structure.
func (structure EffectStructure) Begin() *EffectCursor {
	root := cloneEffectNode(structure.root)
	cursor := &EffectCursor{
		root:         root,
		triggers:     make(map[string]bool),
		conditionals: make(map[string]bool),
	}
	cursor.push(&cursor.root)
	return cursor
}

// SelectAlternative selects the pending closed choice by zero-based alternative index.
func (cursor *EffectCursor) SelectAlternative(choiceID string, alternative int) error {
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

// Consume consumes exactly the next identified step.
func (cursor *EffectCursor) Consume(stepID string, kind EffectStepKind) error {
	_, err := cursor.consume(stepID, kind)
	return err
}

// ConsumeForwardEffect consumes the next forward-effect step and reports
// whether its selected phase requires first-incarnation establishment or a
// later retained-incarnation validation.
func (cursor *EffectCursor) ConsumeForwardEffect(
	stepID string,
) (ForwardEffectCheckpoint, error) {
	checkpoint, err := cursor.consume(stepID, EffectStepForwardEffect)
	if err != nil {
		return 0, err
	}
	if checkpoint == 0 {
		return 0, fmt.Errorf(
			"operationplan: forward effect %q has no enclosing forward phase",
			stepID,
		)
	}
	return checkpoint, nil
}

func (cursor *EffectCursor) consume(
	stepID string,
	kind EffectStepKind,
) (ForwardEffectCheckpoint, error) {
	if err := cursor.requireActive(); err != nil {
		return 0, err
	}
	step, choice, done, err := cursor.next()
	if err != nil {
		return 0, err
	}
	if done {
		return 0, fmt.Errorf("operationplan: effect structure has no remaining step")
	}
	if choice != nil {
		return 0, fmt.Errorf(
			"operationplan: effect choice %q must be selected before consumption",
			choice.node.choiceID,
		)
	}
	if step == nil {
		return 0, fmt.Errorf("operationplan: effect cursor has no consumable step")
	}
	if step.node.step.id != stepID || step.node.step.kind != kind {
		return 0, fmt.Errorf(
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
	checkpoint := ForwardEffectCheckpoint(0)
	if kind == EffectStepForwardEffect {
		phase := cursor.enclosingForwardPhase()
		if phase == nil {
			return 0, fmt.Errorf(
				"operationplan: forward effect %q has no enclosing forward phase",
				stepID,
			)
		}
		checkpoint = ForwardEffectValidateStateDir
		if phase.forwardEffects == 0 {
			checkpoint = ForwardEffectEstablishStateDir
		}
		phase.forwardEffects++
	}
	if kind == EffectStepTerminal {
		cursor.stack = nil
		cursor.terminated = true
	} else {
		cursor.pop()
	}
	return checkpoint, nil
}

// FinishSuccess requires complete selected-path consumption or an explicit
// terminal handoff.
func (cursor *EffectCursor) FinishSuccess() error {
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
	if !cursor.terminated {
		for id, active := range cursor.triggers {
			if active && !cursor.conditionals[id] {
				return fmt.Errorf(
					"operationplan: triggered effect follow-up %q was not reached",
					id,
				)
			}
		}
	}
	cursor.state = effectCursorFinished
	return nil
}

// AbortBeforeEffect terminates an unstarted structure after pure validation work.
func (cursor *EffectCursor) AbortBeforeEffect() error {
	if err := cursor.requireActive(); err != nil {
		return err
	}
	if cursor.terminated {
		return fmt.Errorf("operationplan: effect structure already consumed a terminal handoff")
	}
	if cursor.effectStarted {
		return fmt.Errorf("operationplan: effect structure cannot abort before effect after an effect started")
	}
	cursor.stack = nil
	cursor.state = effectCursorAborted
	return nil
}

func (cursor *EffectCursor) requireActive() error {
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

func (cursor *EffectCursor) next() (
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
		case effectNodeForwardPhase:
			if frame.next != 0 {
				cursor.pop()
				continue
			}
			frame.next = 1
			cursor.push(&frame.node.children[0])
		case effectNodeTrigger:
			if frame.next != 0 {
				cursor.pop()
				continue
			}
			if cursor.conditionals[frame.node.triggerID] {
				return nil, nil, false, fmt.Errorf(
					"operationplan: effect trigger %q was selected after its conditional",
					frame.node.triggerID,
				)
			}
			cursor.triggers[frame.node.triggerID] = true
			frame.next = 1
			cursor.push(&frame.node.children[0])
		case effectNodeConditional:
			if frame.next != 0 {
				cursor.pop()
				continue
			}
			cursor.conditionals[frame.node.triggerID] = true
			if !cursor.triggers[frame.node.triggerID] {
				cursor.pop()
				continue
			}
			frame.next = 1
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

func (cursor *EffectCursor) push(node *EffectNode) {
	cursor.stack = append(cursor.stack, effectCursorFrame{node: node, selected: -1})
}

func (cursor *EffectCursor) pop() {
	cursor.stack = cursor.stack[:len(cursor.stack)-1]
}

func (cursor *EffectCursor) enclosingForwardPhase() *effectCursorFrame {
	for index := len(cursor.stack) - 1; index >= 0; index-- {
		frame := &cursor.stack[index]
		if frame.node.kind == effectNodeForwardPhase {
			return frame
		}
	}
	return nil
}

func (kind EffectStepKind) startsEffect() bool {
	switch kind {
	case EffectStepEstablishStateDir,
		EffectStepForwardEffect,
		EffectStepPublishDescendant,
		EffectStepExternal,
		EffectStepPersistence,
		EffectStepCompensation,
		EffectStepCleanup,
		EffectStepRetirement:
		return true
	default:
		return false
	}
}
