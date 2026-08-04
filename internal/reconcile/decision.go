package reconcile

import (
	"cmp"
	"sort"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// Decision is a derived closed view over projection reconciliation variants.
// It carries no authority beyond the owning Result.
type Decision struct {
	managedPath *ManagedPathDecision
	aggregate   *AggregateSubjectDecision
}

// AggregateSubjectDecision is one subject's read-only view of a physical
// aggregate decision. Physical planning and execution remain owned by the
// enclosing AggregateDecision and occur once per document batch.
type AggregateSubjectDecision struct {
	delta aggregateSubjectDelta
}

// ManagedPath returns the typed managed-path variant when present.
func (decision Decision) ManagedPath() (ManagedPathDecision, bool) {
	if decision.managedPath == nil {
		return ManagedPathDecision{}, false
	}
	return *decision.managedPath, true
}

// Aggregate returns the subject-level aggregate view when present.
func (decision Decision) Aggregate() (AggregateSubjectDecision, bool) {
	if decision.aggregate == nil {
		return AggregateSubjectDecision{}, false
	}
	return *decision.aggregate, true
}

// IsNoOp reports whether this reconciliation result is already current.
func (decision Decision) IsNoOp() bool {
	if decision.managedPath != nil {
		return decision.managedPath.IsNoOp()
	}
	return decision.aggregate != nil && decision.aggregate.IsNoOp()
}

// IsMutation reports whether this reconciliation result changes host or
// durable managed state.
func (decision Decision) IsMutation() bool {
	if decision.managedPath != nil {
		return decision.managedPath.MutatesHost() || decision.managedPath.MutatesState()
	}
	return decision.aggregate != nil &&
		(decision.aggregate.MutatesHost() || decision.aggregate.MutatesState())
}

func (decision AggregateSubjectDecision) Kind() AggregateDecisionKind { return decision.delta.kind }
func (decision AggregateSubjectDecision) Reason() ActionReason        { return decision.delta.reason }

func (decision AggregateSubjectDecision) Detail() string { return decision.delta.detail }

func (decision AggregateSubjectDecision) Subject() topology.SubjectID { return decision.delta.subject }

func (decision AggregateSubjectDecision) Contract() aggregate.ProjectionContract {
	return decision.delta.contract.Clone()
}

func (decision AggregateSubjectDecision) PreviousContribution() (aggregate.ManagedContribution, bool) {
	if !decision.delta.hasPrevious {
		return aggregate.ManagedContribution{}, false
	}
	return decision.delta.previous.Clone(), true
}

// ContributionOccupancy returns fresh codec evidence for this subject when
// the enclosing unmanaged projection could be classified per contribution.
func (decision AggregateSubjectDecision) ContributionOccupancy() (
	aggregate.ContributionOccupancyState,
	bool,
) {
	return decision.delta.occupancy, decision.delta.occupancy != ""
}

func (decision AggregateSubjectDecision) Target() target.Target {
	return decision.delta.contract.Address().Document().Target()
}

func (decision AggregateSubjectDecision) Scope() target.Scope {
	return decision.delta.contract.Address().Document().Scope()
}

func (decision AggregateSubjectDecision) Destination() output.Destination {
	return decision.delta.contract.Address().Document().AggregateRoot()
}

func (decision AggregateSubjectDecision) ContentPath() output.ContentPath {
	return output.ContentPath(decision.delta.contract.Address().ContentPath())
}

func (decision AggregateSubjectDecision) IsBlocked() bool {
	return decision.delta.kind == AggregateBlocked
}

func (decision AggregateSubjectDecision) IsNoOp() bool { return decision.delta.kind == AggregateNoOp }

func (decision AggregateSubjectDecision) MutatesHost() bool {
	return decision.delta.mutatesHost
}

func (decision AggregateSubjectDecision) MutatesState() bool {
	return aggregateSubjectDeltaMutatesState(decision.delta)
}

// SubjectDecisions returns the projection's independently classified subject
// transitions in canonical order.
func (projection AggregateProjectionDecision) SubjectDecisions() []AggregateSubjectDecision {
	result := make([]AggregateSubjectDecision, len(projection.deltas))
	for index, delta := range projection.deltas {
		result[index] = AggregateSubjectDecision{delta: delta}
	}
	return result
}

// Decisions returns every projection decision in one representation-neutral,
// deterministic order owned by the Result rather than by a presenter.
func (result Result) Decisions() []Decision {
	decisions := make([]Decision, 0, result.DecisionCount())
	for _, managedPath := range result.managedPaths {
		copy := managedPath
		decisions = append(decisions, Decision{managedPath: &copy})
	}
	for _, aggregateDecision := range result.aggregates {
		for _, delta := range aggregateDecision.subjectDeltas() {
			copy := AggregateSubjectDecision{delta: delta}
			decisions = append(decisions, Decision{aggregate: &copy})
		}
	}
	sort.SliceStable(decisions, func(left int, right int) bool {
		return compareDecision(decisions[left], decisions[right]) < 0
	})
	return decisions
}

// MutatingDecisions returns every state or host mutation in result-owned display order.
func (result Result) MutatingDecisions() []Decision {
	decisions := result.Decisions()
	mutating := make([]Decision, 0, len(decisions))
	for _, decision := range decisions {
		if decision.IsMutation() {
			mutating = append(mutating, decision)
		}
	}
	return mutating
}

type decisionOrderKey struct {
	phaseRank       int
	destructiveRank int
	targetRank      int
	scope           target.Scope
	destination     string
	entityKind      string
	entityName      string
	subject         topology.SubjectID
	operation       string
	variantRank     int
}

func compareDecision(left Decision, right Decision) int {
	leftKey := decisionKey(left)
	rightKey := decisionKey(right)
	if leftKey.phaseRank != rightKey.phaseRank {
		return cmp.Compare(leftKey.phaseRank, rightKey.phaseRank)
	}
	if leftKey.destructiveRank != rightKey.destructiveRank {
		return cmp.Compare(leftKey.destructiveRank, rightKey.destructiveRank)
	}
	if leftKey.targetRank != rightKey.targetRank {
		return cmp.Compare(leftKey.targetRank, rightKey.targetRank)
	}
	if leftKey.scope != rightKey.scope {
		return cmp.Compare(leftKey.scope, rightKey.scope)
	}
	if leftKey.destination != rightKey.destination {
		return cmp.Compare(leftKey.destination, rightKey.destination)
	}
	if leftKey.entityKind != rightKey.entityKind {
		return cmp.Compare(leftKey.entityKind, rightKey.entityKind)
	}
	if leftKey.entityName != rightKey.entityName {
		return cmp.Compare(leftKey.entityName, rightKey.entityName)
	}
	if compared := topology.CompareSubjectID(leftKey.subject, rightKey.subject); compared != 0 {
		return compared
	}
	if leftKey.operation != rightKey.operation {
		return cmp.Compare(leftKey.operation, rightKey.operation)
	}
	return cmp.Compare(leftKey.variantRank, rightKey.variantRank)
}

func decisionKey(decision Decision) decisionOrderKey {
	if decision.managedPath != nil {
		managedPath := *decision.managedPath
		entityID, _ := topologyprojection.EntityID(managedPath.Subject())
		operation := string(managedPath.Kind().actionKind())
		phaseRank := 0
		if managedPath.Kind() == ManagedPathRemove {
			phaseRank = 3
		}
		return decisionOrderKey{
			phaseRank:       phaseRank,
			destructiveRank: decisionDestructiveRank(operation),
			targetRank:      decisionTargetRank(managedPath.ConsumerTargets(), ""),
			scope:           managedPath.Scope(),
			destination:     managedPath.Destination().String(),
			entityKind:      string(entityID.Kind()),
			entityName:      entityID.Name(),
			subject:         managedPath.Subject(),
			operation:       operation,
			variantRank:     1,
		}
	}
	aggregate := *decision.aggregate
	entityID, _ := topologyprojection.EntityID(aggregate.Subject())
	operation := aggregateOperation(aggregate.Kind())
	return decisionOrderKey{
		phaseRank:       2,
		destructiveRank: decisionDestructiveRank(operation),
		targetRank:      decisionTargetRank(nil, aggregate.Target()),
		scope:           aggregate.Scope(),
		destination:     aggregate.Destination().String(),
		entityKind:      string(entityID.Kind()),
		entityName:      entityID.Name(),
		subject:         aggregate.Subject(),
		operation:       operation,
		variantRank:     2,
	}
}

func decisionTargetRank(consumers []target.Target, fallback target.Target) int {
	rank := targetRank(fallback)
	if fallback == "" {
		rank = len(target.SupportedTargets())
	}
	for _, consumer := range consumers {
		if candidate := targetRank(consumer); candidate < rank {
			rank = candidate
		}
	}
	return rank
}

func targetRank(value target.Target) int {
	for index, supported := range target.SupportedTargets() {
		if value == supported {
			return index
		}
	}
	return len(target.SupportedTargets())
}

func decisionDestructiveRank(operation string) int {
	if operation == string(ActionKindDelete) {
		return 1
	}
	return 0
}

func aggregateOperation(kind AggregateDecisionKind) string {
	switch kind {
	case AggregateCreate:
		return string(ActionKindCreate)
	case AggregateReplace:
		return string(ActionKindUpdate)
	case AggregateRemove:
		return string(ActionKindDelete)
	case AggregateRecord:
		return string(ActionKindRecord)
	case AggregateNoOp:
		return string(ActionKindNoOp)
	case AggregateBlocked:
		return string(ActionKindError)
	default:
		return ""
	}
}
