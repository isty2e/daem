package reconcile

import (
	"cmp"
	"fmt"
	"slices"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// RelationOrderDecisionKind is the closed planner outcome for one physical
// extension load sequence.
type RelationOrderDecisionKind string

const (
	OrderExact                         RelationOrderDecisionKind = "exact"
	OrderNormalize                     RelationOrderDecisionKind = "normalize"
	OrderBlocked                       RelationOrderDecisionKind = "blocked"
	OrderConditionalAfterCarrierChange RelationOrderDecisionKind = "conditional_after_carrier_change"
)

// RelationOrderReason is the stable explanation for a conditional or blocked
// order decision.
type RelationOrderReason string

const (
	OrderReasonNone                   RelationOrderReason = ""
	OrderReasonPendingCarrierInstall  RelationOrderReason = "pending_carrier_install"
	OrderReasonPendingCarrierRemoval  RelationOrderReason = "pending_carrier_removal"
	OrderReasonMembershipMismatch     RelationOrderReason = "membership_mismatch"
	OrderReasonLoadIdentityMismatch   RelationOrderReason = "load_identity_mismatch"
	OrderReasonConflictingCarrierPlan RelationOrderReason = "conflicting_carrier_plan"
	OrderReasonObservationUnavailable RelationOrderReason = "observation_unavailable"
)

// RelationOrderDecisionInput carries one locked desired order, one fresh
// physical observation, and carrier changes that must settle before exact
// normalization can be planned.
type RelationOrderDecisionInput struct {
	Target          target.Target
	Scope           target.Scope
	Constraint      hostrelation.RelationOrderConstraint
	Sequence        observerelation.ObservedRelationSequence
	PendingInstalls []topology.SubjectID
	PendingRemovals []topology.SubjectID
}

// BlockedRelationOrderDecisionInput records a selected physical sequence whose
// host observation could not produce canonical current evidence.
type BlockedRelationOrderDecisionInput struct {
	Target     target.Target
	Scope      target.Scope
	Constraint hostrelation.RelationOrderConstraint
	SequenceID hostrelation.PhysicalSequenceID
	Reason     RelationOrderReason
	Detail     string
}

// RelationOrderDecision is one immutable class/sequence-relative planner fact.
// It owns order classification and risk evidence, not host bytes or mutation
// authority.
type RelationOrderDecision struct {
	target            target.Target
	scope             target.Scope
	constraint        hostrelation.RelationOrderConstraint
	sequence          observerelation.ObservedRelationSequence
	sequenceID        hostrelation.PhysicalSequenceID
	hasSequence       bool
	kind              RelationOrderDecisionKind
	reason            RelationOrderReason
	detail            string
	observedMembers   []hostrelation.RelationOrderMember
	missingMembers    []hostrelation.RelationOrderMember
	pendingInstalls   []topology.SubjectID
	pendingRemovals   []topology.SubjectID
	foreignRows       int
	precedenceChanges []observerelation.PrecedenceChange
}

// NewRelationOrderDecision validates and classifies one physical sequence.
func NewRelationOrderDecision(input RelationOrderDecisionInput) (RelationOrderDecision, error) {
	selectedTarget, err := target.ParseTarget(string(input.Target))
	if err != nil {
		return RelationOrderDecision{}, fmt.Errorf("relation order target: %w", err)
	}
	selectedScope, err := target.ParseScope(string(input.Scope))
	if err != nil {
		return RelationOrderDecision{}, fmt.Errorf("relation order scope: %w", err)
	}
	if err := input.Constraint.Validate(); err != nil {
		return RelationOrderDecision{}, fmt.Errorf("relation order constraint: %w", err)
	}
	if input.Sequence.ClassID() != input.Constraint.ClassID() {
		return RelationOrderDecision{}, fmt.Errorf(
			"relation order sequence class %q does not match constraint %q",
			input.Sequence.ClassID(),
			input.Constraint.ClassID(),
		)
	}
	if err := validateObservedSequence(input.Sequence); err != nil {
		return RelationOrderDecision{}, err
	}

	pendingInstalls, err := canonicalOrderSubjects("pending install", input.PendingInstalls)
	if err != nil {
		return RelationOrderDecision{}, err
	}
	pendingRemovals, err := canonicalOrderSubjects("pending removal", input.PendingRemovals)
	if err != nil {
		return RelationOrderDecision{}, err
	}
	if overlap := firstCommonSubject(pendingInstalls, pendingRemovals); !overlap.IsZero() {
		return RelationOrderDecision{}, fmt.Errorf(
			"relation order subject %q is pending both install and removal",
			overlap,
		)
	}

	decision := RelationOrderDecision{
		target:          selectedTarget,
		scope:           selectedScope,
		constraint:      input.Constraint,
		sequence:        input.Sequence,
		sequenceID:      input.Sequence.SequenceID(),
		hasSequence:     true,
		pendingInstalls: pendingInstalls,
		pendingRemovals: pendingRemovals,
	}
	if err := decision.classify(); err != nil {
		return RelationOrderDecision{}, err
	}
	return decision, nil
}

// NewBlockedRelationOrderDecision constructs one typed observation block
// without fabricating sequence revision evidence.
func NewBlockedRelationOrderDecision(
	input BlockedRelationOrderDecisionInput,
) (RelationOrderDecision, error) {
	selectedTarget, err := target.ParseTarget(string(input.Target))
	if err != nil {
		return RelationOrderDecision{}, fmt.Errorf("blocked relation order target: %w", err)
	}
	selectedScope, err := target.ParseScope(string(input.Scope))
	if err != nil {
		return RelationOrderDecision{}, fmt.Errorf("blocked relation order scope: %w", err)
	}
	if err := input.Constraint.Validate(); err != nil {
		return RelationOrderDecision{}, fmt.Errorf("blocked relation order constraint: %w", err)
	}
	if err := input.SequenceID.Validate(); err != nil {
		return RelationOrderDecision{}, fmt.Errorf("blocked relation order sequence: %w", err)
	}
	if input.Reason != OrderReasonObservationUnavailable {
		return RelationOrderDecision{}, fmt.Errorf(
			"blocked relation order reason %q is unsupported without sequence evidence",
			input.Reason,
		)
	}
	if input.Detail == "" {
		return RelationOrderDecision{}, fmt.Errorf("blocked relation order detail is required")
	}
	return RelationOrderDecision{
		target:     selectedTarget,
		scope:      selectedScope,
		constraint: input.Constraint,
		sequenceID: input.SequenceID,
		kind:       OrderBlocked,
		reason:     input.Reason,
		detail:     input.Detail,
	}, nil
}

func (decision *RelationOrderDecision) classify() error {
	members := decision.constraint.Members()
	memberBySubject := make(map[topology.SubjectID]hostrelation.RelationOrderMember, len(members))
	for _, member := range members {
		memberBySubject[member.Subject()] = member
	}

	observedSubjects := make(map[topology.SubjectID]struct{}, len(members))
	var extraSubjects []topology.SubjectID
	for _, row := range decision.sequence.OrderedRows() {
		subject, correlated := row.CorrelatedSubject()
		if !correlated {
			decision.foreignRows++
			continue
		}
		member, desired := memberBySubject[subject]
		if !desired {
			extraSubjects = append(extraSubjects, subject)
			continue
		}
		if row.HostLoadIdentity() != member.HostLoadIdentity() {
			decision.kind = OrderBlocked
			decision.reason = OrderReasonLoadIdentityMismatch
			return nil
		}
		decision.observedMembers = append(decision.observedMembers, member)
		observedSubjects[subject] = struct{}{}
	}
	for _, member := range members {
		if _, present := observedSubjects[member.Subject()]; !present {
			decision.missingMembers = append(decision.missingMembers, member)
		}
	}

	if !subjectsContained(extraSubjects, decision.pendingRemovals) {
		decision.kind = OrderBlocked
		decision.reason = OrderReasonMembershipMismatch
		return nil
	}
	if !membersContained(decision.missingMembers, decision.pendingInstalls) {
		decision.kind = OrderBlocked
		decision.reason = OrderReasonMembershipMismatch
		return nil
	}
	if installsConflictWithDesired(decision.pendingInstalls, members) ||
		removalsConflictWithDesired(decision.pendingRemovals, members) {
		decision.kind = OrderBlocked
		decision.reason = OrderReasonConflictingCarrierPlan
		return nil
	}
	switch {
	case len(decision.pendingRemovals) != 0:
		decision.kind = OrderConditionalAfterCarrierChange
		decision.reason = OrderReasonPendingCarrierRemoval
		return nil
	case len(decision.missingMembers) != 0 || len(decision.pendingInstalls) != 0:
		decision.kind = OrderConditionalAfterCarrierChange
		decision.reason = OrderReasonPendingCarrierInstall
		return nil
	}

	order, precedenceChanges, err := observerelation.FixedSlotPermutation(
		decision.constraint,
		decision.sequence.OrderedRows(),
	)
	if err != nil {
		return fmt.Errorf("plan fixed-slot relation order: %w", err)
	}
	decision.precedenceChanges = precedenceChanges

	if permutationChangesOrder(order) {
		decision.kind = OrderNormalize
		decision.reason = OrderReasonNone
	} else {
		decision.kind = OrderExact
		decision.reason = OrderReasonNone
	}
	return nil
}

// Validate rejects a zero or forged order decision.
func (decision RelationOrderDecision) Validate() error {
	if !decision.hasSequence {
		rebuilt, err := NewBlockedRelationOrderDecision(BlockedRelationOrderDecisionInput{
			Target:     decision.target,
			Scope:      decision.scope,
			Constraint: decision.constraint,
			SequenceID: decision.sequenceID,
			Reason:     decision.reason,
			Detail:     decision.detail,
		})
		if err != nil {
			return err
		}
		if decision.kind != rebuilt.kind {
			return fmt.Errorf("blocked relation order decision is not canonical")
		}
		return nil
	}
	rebuilt, err := NewRelationOrderDecision(RelationOrderDecisionInput{
		Target:          decision.target,
		Scope:           decision.scope,
		Constraint:      decision.constraint,
		Sequence:        decision.sequence,
		PendingInstalls: decision.pendingInstalls,
		PendingRemovals: decision.pendingRemovals,
	})
	if err != nil {
		return err
	}
	if decision.kind != rebuilt.kind ||
		decision.reason != rebuilt.reason ||
		!slices.Equal(decision.observedMembers, rebuilt.observedMembers) ||
		!slices.Equal(decision.missingMembers, rebuilt.missingMembers) ||
		decision.foreignRows != rebuilt.foreignRows ||
		!slices.Equal(decision.precedenceChanges, rebuilt.precedenceChanges) {
		return fmt.Errorf("relation order decision is not canonical")
	}
	return nil
}

// Compare returns deterministic class then physical-sequence order.
func (decision RelationOrderDecision) Compare(other RelationOrderDecision) int {
	if order := cmp.Compare(decision.constraint.ClassID(), other.constraint.ClassID()); order != 0 {
		return order
	}
	return cmp.Compare(decision.sequenceID, other.sequenceID)
}

func (decision RelationOrderDecision) Target() target.Target { return decision.target }

func (decision RelationOrderDecision) Scope() target.Scope { return decision.scope }

func (decision RelationOrderDecision) ClassID() hostrelation.OrderClassID {
	return decision.constraint.ClassID()
}

func (decision RelationOrderDecision) SequenceID() hostrelation.PhysicalSequenceID {
	return decision.sequenceID
}

func (decision RelationOrderDecision) Authority() observerelation.SequenceAuthority {
	if !decision.hasSequence {
		return ""
	}
	return decision.sequence.Authority()
}

func (decision RelationOrderDecision) Revision() observerelation.SequenceRevision {
	if !decision.hasSequence {
		return ""
	}
	return decision.sequence.Revision()
}

func (decision RelationOrderDecision) RuntimeMeaning() hostrelation.RuntimeMeaning {
	return decision.constraint.RuntimeMeaning()
}

func (decision RelationOrderDecision) ConstraintFingerprint() string {
	return decision.constraint.Fingerprint()
}

func (decision RelationOrderDecision) Kind() RelationOrderDecisionKind { return decision.kind }

func (decision RelationOrderDecision) Reason() RelationOrderReason { return decision.reason }

func (decision RelationOrderDecision) Detail() string { return decision.detail }

func (decision RelationOrderDecision) HasCurrentSequence() bool { return decision.hasSequence }

func (decision RelationOrderDecision) DesiredMembers() []hostrelation.RelationOrderMember {
	return decision.constraint.Members()
}

func (decision RelationOrderDecision) ObservedMembers() []hostrelation.RelationOrderMember {
	return append([]hostrelation.RelationOrderMember(nil), decision.observedMembers...)
}

func (decision RelationOrderDecision) MissingMembers() []hostrelation.RelationOrderMember {
	return append([]hostrelation.RelationOrderMember(nil), decision.missingMembers...)
}

func (decision RelationOrderDecision) ForeignRowCount() int { return decision.foreignRows }

func (decision RelationOrderDecision) PrecedenceChanges() []observerelation.PrecedenceChange {
	return append([]observerelation.PrecedenceChange(nil), decision.precedenceChanges...)
}

func (decision RelationOrderDecision) BlocksOrdinaryApply() bool {
	return decision.kind == OrderBlocked
}

func (decision RelationOrderDecision) RequiresMutation() bool {
	return decision.kind == OrderNormalize
}

func canonicalOrderSubjects(label string, values []topology.SubjectID) ([]topology.SubjectID, error) {
	canonical := append([]topology.SubjectID(nil), values...)
	slices.SortFunc(canonical, topology.CompareSubjectID)
	for index, subject := range canonical {
		if err := subject.Validate(); err != nil {
			return nil, fmt.Errorf("relation order %s[%d]: %w", label, index, err)
		}
		if subject.Kind() != topology.SubjectHostRelation {
			return nil, fmt.Errorf(
				"relation order %s[%d] must be a host relation",
				label,
				index,
			)
		}
		if index != 0 && canonical[index-1] == subject {
			return nil, fmt.Errorf("relation order %s subject %q appears more than once", label, subject)
		}
	}
	return canonical, nil
}

func validateObservedSequence(sequence observerelation.ObservedRelationSequence) error {
	_, err := observerelation.NewObservedRelationSequence(
		sequence.ClassID(),
		sequence.SequenceID(),
		sequence.Authority(),
		sequence.Revision(),
		sequence.OrderedRows(),
	)
	if err != nil {
		return fmt.Errorf("relation order sequence: %w", err)
	}
	return nil
}

func firstCommonSubject(left []topology.SubjectID, right []topology.SubjectID) topology.SubjectID {
	for _, leftSubject := range left {
		for _, rightSubject := range right {
			if leftSubject == rightSubject {
				return leftSubject
			}
		}
	}
	return topology.SubjectID{}
}

func subjectsContained(subjects []topology.SubjectID, allowed []topology.SubjectID) bool {
	for _, subject := range subjects {
		if !slices.Contains(allowed, subject) {
			return false
		}
	}
	return true
}

func membersContained(
	members []hostrelation.RelationOrderMember,
	allowed []topology.SubjectID,
) bool {
	for _, member := range members {
		if !slices.Contains(allowed, member.Subject()) {
			return false
		}
	}
	return true
}

func installsConflictWithDesired(
	installs []topology.SubjectID,
	members []hostrelation.RelationOrderMember,
) bool {
	for _, install := range installs {
		if !memberSubjectPresent(members, install) {
			return true
		}
	}
	return false
}

func removalsConflictWithDesired(
	removals []topology.SubjectID,
	members []hostrelation.RelationOrderMember,
) bool {
	for _, removal := range removals {
		if memberSubjectPresent(members, removal) {
			return true
		}
	}
	return false
}

func memberSubjectPresent(
	members []hostrelation.RelationOrderMember,
	subject topology.SubjectID,
) bool {
	for _, member := range members {
		if member.Subject() == subject {
			return true
		}
	}
	return false
}

func permutationChangesOrder(order []int) bool {
	for destination, source := range order {
		if destination != source {
			return true
		}
	}
	return false
}
