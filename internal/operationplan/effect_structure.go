package operationplan

import (
	"fmt"
	"strings"
)

// EffectStepKind is the closed neutral lifecycle and State Barrier vocabulary.
type EffectStepKind uint8

const (
	// EffectStepEstablishStateDir requests one first-incarnation establishment.
	EffectStepEstablishStateDir EffectStepKind = iota + 1
	// EffectStepValidateBarrier requests one joint journal and file-set validation.
	EffectStepValidateBarrier
	// EffectStepValidateStateDir requests one StateDir identity validation.
	EffectStepValidateStateDir
	// EffectStepForwardEffect marks one StateDir-governed forward effect boundary.
	EffectStepForwardEffect
	// EffectStepBindDescendant binds one reserved StateDir descendant authority.
	EffectStepBindDescendant
	// EffectStepValidateDescendant requests one bound descendant validation.
	EffectStepValidateDescendant
	// EffectStepPublishDescendant requests one bound descendant publication.
	EffectStepPublishDescendant
	// EffectStepExternal marks an external or host-visible effect boundary.
	EffectStepExternal
	// EffectStepObservation marks a post-effect observation.
	EffectStepObservation
	// EffectStepPersistence marks owner-local durable persistence.
	EffectStepPersistence
	// EffectStepCompensation marks owner-local compensation.
	EffectStepCompensation
	// EffectStepCleanup marks owner-local cleanup.
	EffectStepCleanup
	// EffectStepRetirement marks owner-local retirement.
	EffectStepRetirement
	// EffectStepNoOp marks an explicit selected branch with no effect.
	EffectStepNoOp
	// EffectStepTerminal marks an owner-selected terminal handoff that ends the
	// selected structure without authorizing later obligations.
	EffectStepTerminal
)

type effectNodeKind uint8

const (
	effectNodeEmpty effectNodeKind = iota
	effectNodeStep
	effectNodeSequence
	effectNodeChoice
	effectNodeRepeat
	effectNodeForwardPhase
	effectNodeTrigger
	effectNodeConditional
)

type effectStep struct {
	id   string
	kind EffectStepKind
}

// EffectNode is an opaque immutable sequence, choice, repetition, or step input.
type EffectNode struct {
	kind        effectNodeKind
	step        effectStep
	children    []EffectNode
	choiceID    string
	phaseID     string
	triggerID   string
	repetitions int
}

// EffectStructure is one validated, immutable effect obligation structure.
type EffectStructure struct {
	root EffectNode
}

// EffectStructureBuilder accumulates the first construction error while owner
// packages assemble one immutable effect structure. Its zero value is ready.
type EffectStructureBuilder struct {
	err error
}

// Step constructs one uniquely identified neutral effect step.
func (builder *EffectStructureBuilder) Step(id string, kind EffectStepKind) EffectNode {
	if builder == nil || builder.err != nil {
		return EffectNode{}
	}
	step, err := newEffectStep(id, kind)
	if err != nil {
		builder.err = err
		return EffectNode{}
	}
	return step
}

// Choice constructs one closed exclusive choice.
func (builder *EffectStructureBuilder) Choice(
	id string,
	alternatives ...EffectNode,
) EffectNode {
	if builder == nil || builder.err != nil {
		return EffectNode{}
	}
	choice, err := newEffectChoice(id, alternatives...)
	if err != nil {
		builder.err = err
		return EffectNode{}
	}
	return choice
}

// Repeat constructs one compact positive repetition. A terminal handoff ends
// the remaining iterations.
func (builder *EffectStructureBuilder) Repeat(count int, body EffectNode) EffectNode {
	if builder == nil || builder.err != nil {
		return EffectNode{}
	}
	repeated, err := newEffectRepeat(count, body)
	if err != nil {
		builder.err = err
		return EffectNode{}
	}
	return repeated
}

// ForwardPhase groups StateDir-governed forward effects so the first effect
// establishes the incarnation and later effects consume identity checks.
func (builder *EffectStructureBuilder) ForwardPhase(id string, body EffectNode) EffectNode {
	if builder == nil || builder.err != nil {
		return EffectNode{}
	}
	phase, err := newEffectForwardPhase(id, body)
	if err != nil {
		builder.err = err
		return EffectNode{}
	}
	return phase
}

// Trigger marks its body as activating a later conditional follow-up when
// execution reaches it.
func (builder *EffectStructureBuilder) Trigger(id string, body EffectNode) EffectNode {
	if builder == nil || builder.err != nil {
		return EffectNode{}
	}
	trigger, err := newEffectTrigger(id, body)
	if err != nil {
		builder.err = err
		return EffectNode{}
	}
	return trigger
}

// Conditional executes its body when reached after an earlier matching trigger.
func (builder *EffectStructureBuilder) Conditional(id string, body EffectNode) EffectNode {
	if builder == nil || builder.err != nil {
		return EffectNode{}
	}
	conditional, err := newEffectConditional(id, body)
	if err != nil {
		builder.err = err
		return EffectNode{}
	}
	return conditional
}

// Compile returns the first construction error or validates the complete structure.
func (builder *EffectStructureBuilder) Compile(root EffectNode) (EffectStructure, error) {
	if builder == nil {
		return EffectStructure{}, fmt.Errorf("operationplan: effect structure builder is unavailable")
	}
	if builder.err != nil {
		return EffectStructure{}, builder.err
	}
	return compileEffectStructure(root)
}

func newEffectStep(id string, kind EffectStepKind) (EffectNode, error) {
	if err := validateEffectReference("step", id); err != nil {
		return EffectNode{}, err
	}
	if !kind.valid() {
		return EffectNode{}, fmt.Errorf("operationplan: effect step %q has invalid kind %d", id, kind)
	}
	return EffectNode{kind: effectNodeStep, step: effectStep{id: id, kind: kind}}, nil
}

// EffectSequence composes children in order until a terminal handoff. No
// children is no work.
func EffectSequence(children ...EffectNode) EffectNode {
	if len(children) == 0 {
		return EffectNode{kind: effectNodeEmpty}
	}
	return EffectNode{kind: effectNodeSequence, children: cloneEffectNodes(children)}
}

func newEffectChoice(id string, alternatives ...EffectNode) (EffectNode, error) {
	if err := validateEffectReference("choice", id); err != nil {
		return EffectNode{}, err
	}
	if len(alternatives) < 2 {
		return EffectNode{}, fmt.Errorf("operationplan: effect choice %q requires at least two alternatives", id)
	}
	return EffectNode{
		kind:     effectNodeChoice,
		children: cloneEffectNodes(alternatives),
		choiceID: id,
	}, nil
}

func newEffectRepeat(count int, body EffectNode) (EffectNode, error) {
	if count < 0 {
		return EffectNode{}, fmt.Errorf("operationplan: effect repetition must not be negative")
	}
	if count == 0 {
		return EffectNode{kind: effectNodeEmpty}, nil
	}
	return EffectNode{
		kind:        effectNodeRepeat,
		children:    []EffectNode{cloneEffectNode(body)},
		repetitions: count,
	}, nil
}

func newEffectForwardPhase(id string, body EffectNode) (EffectNode, error) {
	if err := validateEffectReference("forward phase", id); err != nil {
		return EffectNode{}, err
	}
	return EffectNode{
		kind:     effectNodeForwardPhase,
		children: []EffectNode{cloneEffectNode(body)},
		phaseID:  id,
	}, nil
}

func newEffectTrigger(id string, body EffectNode) (EffectNode, error) {
	if err := validateEffectReference("trigger", id); err != nil {
		return EffectNode{}, err
	}
	return EffectNode{
		kind:      effectNodeTrigger,
		children:  []EffectNode{cloneEffectNode(body)},
		triggerID: id,
	}, nil
}

func newEffectConditional(id string, body EffectNode) (EffectNode, error) {
	if err := validateEffectReference("conditional", id); err != nil {
		return EffectNode{}, err
	}
	return EffectNode{
		kind:      effectNodeConditional,
		children:  []EffectNode{cloneEffectNode(body)},
		triggerID: id,
	}, nil
}

type effectValidation struct {
	stepIDs          map[string]struct{}
	choiceIDs        map[string]struct{}
	phaseIDs         map[string]struct{}
	triggerProducers map[string]int
	conditionals     map[string]struct{}
}

func compileEffectStructure(root EffectNode) (EffectStructure, error) {
	canonical := cloneEffectNode(root)
	validation := effectValidation{
		stepIDs:          make(map[string]struct{}),
		choiceIDs:        make(map[string]struct{}),
		phaseIDs:         make(map[string]struct{}),
		triggerProducers: make(map[string]int),
		conditionals:     make(map[string]struct{}),
	}
	if err := validateEffectNode(canonical, &validation, false); err != nil {
		return EffectStructure{}, err
	}
	for id := range validation.conditionals {
		if validation.triggerProducers[id] == 0 {
			return EffectStructure{}, fmt.Errorf(
				"operationplan: effect conditional %q has no trigger",
				id,
			)
		}
	}
	for id := range validation.triggerProducers {
		if _, present := validation.conditionals[id]; !present {
			return EffectStructure{}, fmt.Errorf(
				"operationplan: effect trigger %q has no conditional follow-up",
				id,
			)
		}
	}
	return EffectStructure{root: canonical}, nil
}

func validateEffectNode(
	node EffectNode,
	validation *effectValidation,
	insideForwardPhase bool,
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
		if node.step.kind == EffectStepForwardEffect && !insideForwardPhase {
			return fmt.Errorf(
				"operationplan: forward effect step %q is outside a forward phase",
				node.step.id,
			)
		}
		if _, duplicate := validation.stepIDs[node.step.id]; duplicate {
			return fmt.Errorf("operationplan: duplicate effect step %q", node.step.id)
		}
		validation.stepIDs[node.step.id] = struct{}{}
		return nil
	case effectNodeSequence:
		if len(node.children) == 0 {
			return fmt.Errorf("operationplan: effect sequence is empty")
		}
		for _, child := range node.children {
			if err := validateEffectNode(child, validation, insideForwardPhase); err != nil {
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
		if _, duplicate := validation.choiceIDs[node.choiceID]; duplicate {
			return fmt.Errorf("operationplan: duplicate effect choice %q", node.choiceID)
		}
		validation.choiceIDs[node.choiceID] = struct{}{}
		for _, child := range node.children {
			if err := validateEffectNode(child, validation, insideForwardPhase); err != nil {
				return err
			}
		}
		return nil
	case effectNodeRepeat:
		if node.repetitions <= 0 || len(node.children) != 1 {
			return fmt.Errorf("operationplan: effect repetition is invalid")
		}
		return validateEffectNode(node.children[0], validation, insideForwardPhase)
	case effectNodeForwardPhase:
		if insideForwardPhase {
			return fmt.Errorf("operationplan: forward effect phases must not nest")
		}
		if err := validateEffectReference("forward phase", node.phaseID); err != nil {
			return err
		}
		if len(node.children) != 1 {
			return fmt.Errorf("operationplan: forward effect phase %q is invalid", node.phaseID)
		}
		if _, duplicate := validation.phaseIDs[node.phaseID]; duplicate {
			return fmt.Errorf("operationplan: duplicate forward effect phase %q", node.phaseID)
		}
		validation.phaseIDs[node.phaseID] = struct{}{}
		return validateEffectNode(node.children[0], validation, true)
	case effectNodeTrigger:
		if err := validateEffectReference("trigger", node.triggerID); err != nil {
			return err
		}
		if len(node.children) != 1 {
			return fmt.Errorf("operationplan: effect trigger %q is invalid", node.triggerID)
		}
		validation.triggerProducers[node.triggerID]++
		return validateEffectNode(node.children[0], validation, insideForwardPhase)
	case effectNodeConditional:
		if err := validateEffectReference("conditional", node.triggerID); err != nil {
			return err
		}
		if len(node.children) != 1 {
			return fmt.Errorf("operationplan: effect conditional %q is invalid", node.triggerID)
		}
		if _, duplicate := validation.conditionals[node.triggerID]; duplicate {
			return fmt.Errorf("operationplan: duplicate effect conditional %q", node.triggerID)
		}
		validation.conditionals[node.triggerID] = struct{}{}
		return validateEffectNode(node.children[0], validation, insideForwardPhase)
	default:
		return fmt.Errorf("operationplan: effect structure has invalid node kind %d", node.kind)
	}
}

func (kind EffectStepKind) valid() bool {
	return kind >= EffectStepEstablishStateDir && kind <= EffectStepTerminal
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

func cloneEffectNodes(nodes []EffectNode) []EffectNode {
	result := make([]EffectNode, len(nodes))
	for index, node := range nodes {
		result[index] = cloneEffectNode(node)
	}
	return result
}

func cloneEffectNode(node EffectNode) EffectNode {
	node.children = cloneEffectNodes(node.children)
	return node
}

// Equal reports exact structural equality, including references, order,
// alternatives, repetitions, forward phases, and follow-up relations.
func (structure EffectStructure) Equal(other EffectStructure) bool {
	return equalEffectNode(structure.root, other.root)
}

func equalEffectNode(left EffectNode, right EffectNode) bool {
	if left.kind != right.kind ||
		left.step != right.step ||
		left.choiceID != right.choiceID ||
		left.phaseID != right.phaseID ||
		left.triggerID != right.triggerID ||
		left.repetitions != right.repetitions ||
		len(left.children) != len(right.children) {
		return false
	}
	for index := range left.children {
		if !equalEffectNode(left.children[index], right.children[index]) {
			return false
		}
	}
	return true
}
