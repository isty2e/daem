package readiness

import (
	"fmt"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func observeExtensionOrders(
	paths daempaths.Paths,
	locked lock.File,
	selectedTargets reconcile.SelectedTargets,
	relationActions []reconcile.RelationAction,
	absenceActions []carrierabsence.Action,
) ([]reconcile.RelationOrderDecision, error) {
	decisions := make([]reconcile.RelationOrderDecision, 0)
	for _, constraint := range locked.Locked.OrderConstraints() {
		selectedTarget, capability, admitted := profile.ExtensionOrderCapabilityForClass(
			constraint.ClassID(),
		)
		if !admitted {
			return nil, fmt.Errorf(
				"locked extension order class %q has no unique profile owner",
				constraint.ClassID(),
			)
		}
		if !selectedTargets.Contains(selectedTarget) {
			continue
		}

		pendingInstalls := pendingOrderInstalls(
			relationActions,
			selectedTarget,
			capability.Scope(),
			capability.Carrier(),
		)
		pendingRemovals := pendingOrderRemovals(
			absenceActions,
			selectedTarget,
			capability.Scope(),
			capability.Carrier(),
		)
		observation, err := relationhost.ObserveOrder(relationhost.OrderInput{
			Paths:      paths,
			Lockfile:   locked,
			Constraint: constraint,
		})
		if err != nil {
			blocked, blockErr := blockedOrderDecisions(
				selectedTarget,
				capability,
				constraint,
				err,
			)
			if blockErr != nil {
				return nil, blockErr
			}
			decisions = append(decisions, blocked...)
			continue
		}
		observedDecisions, err := DecideExtensionOrderObservation(
			selectedTarget,
			capability,
			constraint,
			observation,
			pendingInstalls,
			pendingRemovals,
		)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, observedDecisions...)
	}
	return decisions, nil
}

// DecideExtensionOrderObservation classifies every meaningful physical
// sequence according to the capability's membership contract. Subset
// sequences retain members they load plus members whose install is pending.
func DecideExtensionOrderObservation(
	selectedTarget target.Target,
	capability profile.ExtensionOrderCapability,
	constraint hostrelation.RelationOrderConstraint,
	observation relationhost.OrderObservation,
	pendingInstalls []topology.SubjectID,
	pendingRemovals []topology.SubjectID,
) ([]reconcile.RelationOrderDecision, error) {
	decisions := make([]reconcile.RelationOrderDecision, 0, len(observation.Physical()))
	for _, physical := range observation.Physical() {
		physicalConstraint := constraint
		if capability.SequenceMembership() == profile.LoadedClassSubset {
			var admitted bool
			var err error
			physicalConstraint, admitted, err = projectPhysicalOrderConstraint(
				constraint,
				physical,
				pendingInstalls,
			)
			if err != nil {
				return nil, err
			}
			if !admitted {
				continue
			}
		}
		decision, err := reconcile.NewRelationOrderDecision(
			reconcile.RelationOrderDecisionInput{
				Target:          selectedTarget,
				Scope:           capability.Scope(),
				Constraint:      physicalConstraint,
				Sequence:        physical.Sequence(),
				PendingInstalls: pendingInstalls,
				PendingRemovals: pendingRemovals,
			},
		)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return decisions, nil
}

func projectPhysicalOrderConstraint(
	constraint hostrelation.RelationOrderConstraint,
	sequence relationhost.PhysicalOrderObservation,
	pendingInstalls []topology.SubjectID,
) (hostrelation.RelationOrderConstraint, bool, error) {
	selected := make(map[topology.SubjectID]struct{})
	for _, row := range sequence.Sequence().OrderedRows() {
		if subject, correlated := row.CorrelatedSubject(); correlated {
			selected[subject] = struct{}{}
		}
	}
	for _, subject := range pendingInstalls {
		selected[subject] = struct{}{}
	}

	members := make([]hostrelation.RelationOrderMember, 0, len(selected))
	for _, member := range constraint.Members() {
		if _, present := selected[member.Subject()]; present {
			members = append(members, member)
		}
	}
	if len(members) == 0 {
		return hostrelation.RelationOrderConstraint{}, false, nil
	}
	projected, err := hostrelation.NewRelationOrderConstraint(
		constraint.ClassID(),
		constraint.MemberIdentityContract(),
		constraint.RuntimeMeaning(),
		members,
	)
	if err != nil {
		return hostrelation.RelationOrderConstraint{}, false, fmt.Errorf(
			"project physical extension order: %w",
			err,
		)
	}
	return projected, true, nil
}

func pendingOrderInstalls(
	actions []reconcile.RelationAction,
	selectedTarget target.Target,
	scope target.Scope,
	carrier desiredextension.Carrier,
) []topology.SubjectID {
	subjects := make([]topology.SubjectID, 0)
	for _, action := range actions {
		if action.Target() != selectedTarget ||
			action.Scope() != scope ||
			action.CarrierIdentity().Carrier().Family() != carrier ||
			!action.InvokesHostRoute() {
			continue
		}
		subjects = append(subjects, action.Subject())
	}
	return subjects
}

func pendingOrderRemovals(
	actions []carrierabsence.Action,
	selectedTarget target.Target,
	scope target.Scope,
	carrier desiredextension.Carrier,
) []topology.SubjectID {
	subjects := make([]topology.SubjectID, 0)
	for _, action := range actions {
		if action.Target() != selectedTarget ||
			action.Scope() != scope ||
			action.Claim().Identity().Carrier().Family() != carrier {
			continue
		}
		switch action.Decision() {
		case carrierabsence.DecisionRemove, carrierabsence.DecisionVerifyPendingRemoval:
			subjects = append(subjects, action.Subject())
		}
	}
	return subjects
}

func blockedOrderDecisions(
	selectedTarget target.Target,
	capability profile.ExtensionOrderCapability,
	constraint hostrelation.RelationOrderConstraint,
	observationErr error,
) ([]reconcile.RelationOrderDecision, error) {
	decisions := make([]reconcile.RelationOrderDecision, 0, len(capability.PhysicalSequenceIDs()))
	for _, sequenceID := range capability.PhysicalSequenceIDs() {
		decision, err := reconcile.NewBlockedRelationOrderDecision(
			reconcile.BlockedRelationOrderDecisionInput{
				Target:     selectedTarget,
				Scope:      capability.Scope(),
				Constraint: constraint,
				SequenceID: sequenceID,
				Reason: reconcile.RelationOrderObservationFailureReason(
					observationErr,
				),
				Detail: observationErr.Error(),
			},
		)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return decisions, nil
}
