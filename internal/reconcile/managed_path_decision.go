package reconcile

import (
	"fmt"
	"os"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// ManagedPathDecisionKind is the closed reconciliation result for one physical
// managed path occupancy.
type ManagedPathDecisionKind string

const (
	ManagedPathCreate  ManagedPathDecisionKind = "create"
	ManagedPathReplace ManagedPathDecisionKind = "replace"
	ManagedPathRemove  ManagedPathDecisionKind = "remove"
	ManagedPathRecord  ManagedPathDecisionKind = "record"
	ManagedPathNoOp    ManagedPathDecisionKind = "noop"
	ManagedPathBlocked ManagedPathDecisionKind = "blocked"
)

type managedPathDecisionClassification struct {
	actionKind   ActionKind
	mutatesHost  bool
	mutatesState bool
}

func (kind ManagedPathDecisionKind) classification() managedPathDecisionClassification {
	switch kind {
	case ManagedPathCreate:
		return managedPathDecisionClassification{
			actionKind: ActionKindCreate, mutatesHost: true, mutatesState: true,
		}
	case ManagedPathReplace:
		return managedPathDecisionClassification{
			actionKind: ActionKindUpdate, mutatesHost: true, mutatesState: true,
		}
	case ManagedPathRemove:
		return managedPathDecisionClassification{
			actionKind: ActionKindDelete, mutatesHost: true, mutatesState: true,
		}
	case ManagedPathRecord:
		return managedPathDecisionClassification{
			actionKind: ActionKindRecord, mutatesState: true,
		}
	case ManagedPathNoOp:
		return managedPathDecisionClassification{actionKind: ActionKindNoOp}
	case ManagedPathBlocked:
		return managedPathDecisionClassification{actionKind: ActionKindError}
	default:
		panic(fmt.Sprintf("managed path decision kind %q has no classification", kind))
	}
}

// MutatesHost reports whether this decision kind changes host state.
// It panics for unsupported kinds, which must not escape canonical construction.
func (kind ManagedPathDecisionKind) MutatesHost() bool {
	return kind.classification().mutatesHost
}

// MutatesState reports whether this decision kind changes durable managed state.
// It panics for unsupported kinds, which must not escape canonical construction.
func (kind ManagedPathDecisionKind) MutatesState() bool {
	return kind.classification().mutatesState
}

func (kind ManagedPathDecisionKind) actionKind() ActionKind {
	return kind.classification().actionKind
}

type managedPathDecisionFacts struct {
	subject          topology.SubjectID
	consumerTargets  []target.Target
	scope            target.Scope
	destination      output.Destination
	desiredHash      artifact.ContentHash
	liveHash         artifact.ContentHash
	contentKind      realization.PathProjectionContentKind
	placementMode    realization.PathProjectionMode
	permissionPolicy realization.PathPermissionPolicy
	desiredFileMode  os.FileMode
	liveFileMode     os.FileMode
	previous         *durable.ManagedPathState
	reason           ActionReason
	detail           string
}

// ManagedPathDecisionInput carries the facts used to construct one managed-path
// decision. Callers may assemble it incrementally; only successful construction
// admits a canonical decision.
type ManagedPathDecisionInput struct {
	Kind             ManagedPathDecisionKind
	Subject          topology.SubjectID
	ConsumerTargets  []target.Target
	Scope            target.Scope
	Destination      output.Destination
	DesiredHash      artifact.ContentHash
	LiveHash         artifact.ContentHash
	ContentKind      realization.PathProjectionContentKind
	PlacementMode    realization.PathProjectionMode
	PermissionPolicy realization.PathPermissionPolicy
	DesiredFileMode  os.FileMode
	LiveFileMode     os.FileMode
	Previous         *durable.ManagedPathState
	Reason           ActionReason
	Detail           string
}

type (
	managedPathCreateDecision  struct{ facts managedPathDecisionFacts }
	managedPathReplaceDecision struct{ facts managedPathDecisionFacts }
	managedPathRemoveDecision  struct{ facts managedPathDecisionFacts }
	managedPathRecordDecision  struct{ facts managedPathDecisionFacts }
	managedPathNoOpDecision    struct{ facts managedPathDecisionFacts }
	managedPathBlockedDecision struct{ facts managedPathDecisionFacts }
)

// ManagedPathDecision is a closed union. Exactly one private variant is present.
type ManagedPathDecision struct {
	create  *managedPathCreateDecision
	replace *managedPathReplaceDecision
	remove  *managedPathRemoveDecision
	record  *managedPathRecordDecision
	noOp    *managedPathNoOpDecision
	blocked *managedPathBlockedDecision
}

// NewManagedPathDecision constructs one validated closed managed-path variant.
func NewManagedPathDecision(input ManagedPathDecisionInput) (ManagedPathDecision, error) {
	previous := input.Previous
	if previous != nil {
		copy := *previous
		previous = &copy
	}
	facts := managedPathDecisionFacts{
		subject:          input.Subject,
		consumerTargets:  append([]target.Target(nil), input.ConsumerTargets...),
		scope:            input.Scope,
		destination:      input.Destination,
		desiredHash:      input.DesiredHash,
		liveHash:         input.LiveHash,
		contentKind:      input.ContentKind,
		placementMode:    input.PlacementMode,
		permissionPolicy: input.PermissionPolicy,
		desiredFileMode:  input.DesiredFileMode,
		liveFileMode:     input.LiveFileMode,
		previous:         previous,
		reason:           input.Reason,
		detail:           input.Detail,
	}

	var decision ManagedPathDecision
	switch input.Kind {
	case ManagedPathCreate:
		decision = ManagedPathDecision{create: &managedPathCreateDecision{facts: facts}}
	case ManagedPathReplace:
		decision = ManagedPathDecision{replace: &managedPathReplaceDecision{facts: facts}}
	case ManagedPathRemove:
		decision = ManagedPathDecision{remove: &managedPathRemoveDecision{facts: facts}}
	case ManagedPathRecord:
		decision = ManagedPathDecision{record: &managedPathRecordDecision{facts: facts}}
	case ManagedPathNoOp:
		decision = ManagedPathDecision{noOp: &managedPathNoOpDecision{facts: facts}}
	case ManagedPathBlocked:
		decision = ManagedPathDecision{blocked: &managedPathBlockedDecision{facts: facts}}
	default:
		return ManagedPathDecision{}, fmt.Errorf("managed path decision kind %q is unsupported", input.Kind)
	}
	_ = input.Kind.classification()
	if err := validateActionReason(input.Reason); err != nil {
		return ManagedPathDecision{}, fmt.Errorf("managed path decision: %w", err)
	}
	if err := validateManagedPathDecision(decision); err != nil {
		return ManagedPathDecision{}, err
	}
	return decision, nil
}

func newManagedPathCreate(facts managedPathDecisionFacts, reason ActionReason) ManagedPathDecision {
	facts.reason = reason
	return ManagedPathDecision{create: &managedPathCreateDecision{facts: facts}}
}

func newManagedPathReplace(facts managedPathDecisionFacts, reason ActionReason, detail string) ManagedPathDecision {
	facts.reason, facts.detail = reason, detail
	return ManagedPathDecision{replace: &managedPathReplaceDecision{facts: facts}}
}

func newManagedPathRemove(facts managedPathDecisionFacts, reason ActionReason) ManagedPathDecision {
	facts.reason = reason
	return ManagedPathDecision{remove: &managedPathRemoveDecision{facts: facts}}
}

func newManagedPathRecord(facts managedPathDecisionFacts, reason ActionReason, detail string) ManagedPathDecision {
	facts.reason, facts.detail = reason, detail
	return ManagedPathDecision{record: &managedPathRecordDecision{facts: facts}}
}

func newManagedPathNoOp(facts managedPathDecisionFacts, reason ActionReason) ManagedPathDecision {
	facts.reason = reason
	return ManagedPathDecision{noOp: &managedPathNoOpDecision{facts: facts}}
}

func newManagedPathBlocked(facts managedPathDecisionFacts, reason ActionReason, detail string) ManagedPathDecision {
	facts.reason, facts.detail = reason, detail
	return ManagedPathDecision{blocked: &managedPathBlockedDecision{facts: facts}}
}

func (decision ManagedPathDecision) facts() managedPathDecisionFacts {
	switch {
	case decision.create != nil:
		return decision.create.facts
	case decision.replace != nil:
		return decision.replace.facts
	case decision.remove != nil:
		return decision.remove.facts
	case decision.record != nil:
		return decision.record.facts
	case decision.noOp != nil:
		return decision.noOp.facts
	case decision.blocked != nil:
		return decision.blocked.facts
	default:
		return managedPathDecisionFacts{}
	}
}

func (decision ManagedPathDecision) Kind() ManagedPathDecisionKind {
	switch {
	case decision.create != nil:
		return ManagedPathCreate
	case decision.replace != nil:
		return ManagedPathReplace
	case decision.remove != nil:
		return ManagedPathRemove
	case decision.record != nil:
		return ManagedPathRecord
	case decision.noOp != nil:
		return ManagedPathNoOp
	case decision.blocked != nil:
		return ManagedPathBlocked
	default:
		return ""
	}
}

func (decision ManagedPathDecision) Subject() topology.SubjectID { return decision.facts().subject }
func (decision ManagedPathDecision) ConsumerTargets() []target.Target {
	return append([]target.Target(nil), decision.facts().consumerTargets...)
}
func (decision ManagedPathDecision) Scope() target.Scope { return decision.facts().scope }
func (decision ManagedPathDecision) Destination() output.Destination {
	return decision.facts().destination
}

func (decision ManagedPathDecision) DesiredHash() artifact.ContentHash {
	return decision.facts().desiredHash
}

func (decision ManagedPathDecision) LiveHash() artifact.ContentHash { return decision.facts().liveHash }

func (decision ManagedPathDecision) ContentKind() realization.PathProjectionContentKind {
	return decision.facts().contentKind
}

func (decision ManagedPathDecision) PlacementMode() realization.PathProjectionMode {
	return decision.facts().placementMode
}

func (decision ManagedPathDecision) PermissionPolicy() realization.PathPermissionPolicy {
	return decision.facts().permissionPolicy
}

func (decision ManagedPathDecision) DesiredFileMode() os.FileMode {
	return decision.facts().desiredFileMode
}

func (decision ManagedPathDecision) LiveFileMode() os.FileMode {
	return decision.facts().liveFileMode
}
func (decision ManagedPathDecision) Reason() ActionReason { return decision.facts().reason }
func (decision ManagedPathDecision) Detail() string       { return decision.facts().detail }
func (decision ManagedPathDecision) PreviousState() (durable.ManagedPathState, bool) {
	previous := decision.facts().previous
	if previous == nil {
		return durable.ManagedPathState{}, false
	}
	return *previous, true
}

// InvolvesScope reports whether the decision's current or previous physical
// occupancy belongs to scope.
func (decision ManagedPathDecision) InvolvesScope(scope target.Scope) bool {
	if decision.Scope() == scope {
		return true
	}
	previous, present := decision.PreviousState()
	return present && previous.Scope() == scope
}

func (decision ManagedPathDecision) IsBlocked() bool { return decision.Kind() == ManagedPathBlocked }
func (decision ManagedPathDecision) IsNoOp() bool    { return decision.Kind() == ManagedPathNoOp }
func (decision ManagedPathDecision) IsPending() bool { return !decision.IsNoOp() }

func (decision ManagedPathDecision) MutatesHost() bool {
	return decision.Kind().MutatesHost()
}

func (decision ManagedPathDecision) MutatesState() bool {
	return decision.Kind().MutatesState()
}

func validateManagedPathDecision(decision ManagedPathDecision) error {
	variants := 0
	for _, present := range []bool{
		decision.create != nil,
		decision.replace != nil,
		decision.remove != nil,
		decision.record != nil,
		decision.noOp != nil,
		decision.blocked != nil,
	} {
		if present {
			variants++
		}
	}
	if variants != 1 {
		return fmt.Errorf("requires exactly one variant, got %d", variants)
	}

	facts := decision.facts()
	if err := facts.subject.Validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if facts.subject.Kind() != topology.SubjectProjection {
		return fmt.Errorf("subject %q is not a projection", facts.subject)
	}
	if _, err := target.ParseScope(string(facts.scope)); err != nil {
		return fmt.Errorf("scope: %w", err)
	}
	if err := facts.destination.ValidateScope(facts.scope); err != nil {
		return fmt.Errorf("destination: %w", err)
	}
	if len(facts.consumerTargets) == 0 && facts.previous == nil {
		return fmt.Errorf("requires a current consumer target or previous managed state")
	}
	seenTargets := make(map[target.Target]struct{}, len(facts.consumerTargets))
	for index, consumer := range facts.consumerTargets {
		parsed, err := target.ParseTarget(string(consumer))
		if err != nil {
			return fmt.Errorf("consumer target[%d]: %w", index, err)
		}
		if _, duplicate := seenTargets[parsed]; duplicate {
			return fmt.Errorf("duplicate consumer target %q", parsed)
		}
		seenTargets[parsed] = struct{}{}
	}
	if facts.previous != nil && facts.previous.Subject() != facts.subject {
		if decision.Kind() != ManagedPathReplace && decision.Kind() != ManagedPathBlocked {
			return fmt.Errorf("cross-subject previous state requires a replace or blocked decision")
		}
		currentEntity, currentEntityBacked := topologyprojection.EntityID(facts.subject)
		previousEntity, previousEntityBacked := topologyprojection.EntityID(facts.previous.Subject())
		if !currentEntityBacked || !previousEntityBacked || currentEntity != previousEntity {
			return fmt.Errorf("cross-subject previous state must belong to the same entity")
		}
		if facts.previous.Scope() == facts.scope && facts.previous.Destination() == facts.destination {
			return fmt.Errorf("cross-subject previous state requires a physical relocation")
		}
		if facts.previous.ContentKind() != facts.contentKind {
			return fmt.Errorf("cross-subject previous state content kind does not match")
		}
		if !slices.Equal(facts.previous.ConsumerTargets(), facts.consumerTargets) {
			return fmt.Errorf("cross-subject previous state consumers do not match")
		}
	}
	return nil
}
