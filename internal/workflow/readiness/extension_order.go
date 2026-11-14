package readiness

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type extensionOrderObservation struct {
	selectedTarget target.Target
	capability     profile.ExtensionOrderCapability
	constraint     hostrelation.RelationOrderConstraint
	observation    relationhost.OrderObservation
	observeErr     error
}

func observeClasslessRetirementEdits(
	paths daempaths.Paths,
	locked lock.File,
	retiring []durablecarrier.ManagedCarrierClaim,
) ([]relationhost.SelectedDocumentEdit, error) {
	if len(retiring) == 0 {
		return nil, nil
	}
	var edits []relationhost.SelectedDocumentEdit
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(target.TargetOpenCode, scope, desiredextension.CarrierOpenCodePlugin)
		if !admitted {
			continue
		}
		if _, lockedClass := locked.Locked.OrderConstraint(capability.ClassID()); lockedClass {
			continue
		}
		selected, err := relationhost.ObserveOpenCodeRetirementEdits(paths, locked, scope, retiring)
		if err != nil {
			return nil, err
		}
		edits = append(edits, selected...)
	}
	return edits, nil
}

func observeExtensionOrders(
	paths daempaths.Paths,
	locked lock.File,
	selectedTargets reconcile.SelectedTargets,
	relationActions []reconcile.RelationAction,
	absenceActions []carrierabsence.Action,
) ([]reconcile.RelationOrderDecision, error) {
	facts, err := observeExtensionOrderFacts(paths, locked, selectedTargets)
	if err != nil {
		return nil, err
	}
	return planExtensionOrderDecisions(facts, relationActions, absenceActions)
}

func observeExtensionOrderFacts(
	paths daempaths.Paths,
	locked lock.File,
	selectedTargets reconcile.SelectedTargets,
	native ...nativeAssessmentSources,
) ([]extensionOrderObservation, error) {
	facts := make([]extensionOrderObservation, 0)
	for _, constraint := range locked.Locked.OrderConstraints() {
		selectedTarget, capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapabilityForClass(
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
		input := relationhost.OrderInput{
			Paths:      paths,
			Lockfile:   locked,
			Constraint: constraint,
		}
		if selectedTarget == target.TargetPi {
			alias, err := pipackage.SettingsDocumentAddress(capability.Scope())
			if err != nil {
				return nil, err
			}
			if document, _, retained := retainedNativeDocument(native, alias); retained {
				input.PiDocument = &document
			}
		}
		observation, err := relationhost.ObserveOrder(input)
		facts = append(facts, extensionOrderObservation{
			selectedTarget: selectedTarget,
			capability:     capability,
			constraint:     constraint,
			observation:    observation,
			observeErr:     err,
		})
	}
	return facts, nil
}

func planExtensionOrderDecisions(
	facts []extensionOrderObservation,
	relationActions []reconcile.RelationAction,
	absenceActions []carrierabsence.Action,
) ([]reconcile.RelationOrderDecision, error) {
	decisions := make([]reconcile.RelationOrderDecision, 0)
	for _, fact := range facts {
		if fact.observeErr != nil {
			blocked, blockErr := blockedOrderDecisions(
				fact.selectedTarget,
				fact.capability,
				fact.constraint,
				fact.observeErr,
			)
			if blockErr != nil {
				return nil, blockErr
			}
			decisions = append(decisions, blocked...)
			continue
		}
		pendingInstalls := pendingOrderInstalls(
			relationActions,
			fact.selectedTarget,
			fact.capability.Scope(),
			fact.capability.Carrier(),
		)
		pendingRemovals := pendingOrderRemovals(
			absenceActions,
			fact.selectedTarget,
			fact.capability.Scope(),
			fact.capability.Carrier(),
		)
		observedDecisions, err := DecideExtensionOrderObservation(
			fact.selectedTarget,
			fact.capability,
			fact.constraint,
			fact.observation,
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
				physical.Sequence().OrderedRows(),
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

// DecideSelectedExtensionOrder classifies a native combined edit using the
// same per-document membership projection as ordinary order planning.
// Retirement-only and empty loaded subsets have no order decision.
func DecideSelectedExtensionOrder(
	edit relationhost.SelectedDocumentEdit,
	actions []reconcile.RelationAction,
) (reconcile.RelationOrderDecision, bool, error) {
	if err := edit.Validate(); err != nil {
		return reconcile.RelationOrderDecision{}, false, err
	}
	selected := edit.SelectedSequence()
	constraint, ordered := selected.Constraint()
	if !ordered {
		return reconcile.RelationOrderDecision{}, false, nil
	}
	selectedTarget, capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapabilityForClass(constraint.ClassID())
	if !admitted || selectedTarget != edit.DocumentAddress().Target() || capability.Scope() != edit.Scope() ||
		!capability.AdmitsPhysicalSequenceID(selected.Original().SequenceID()) {
		return reconcile.RelationOrderDecision{}, false, fmt.Errorf("selected order edit has no matching surface capability")
	}
	pendingInstalls := pendingOrderInstalls(actions, selectedTarget, capability.Scope(), capability.Carrier())
	if capability.SequenceMembership() == profile.LoadedClassSubset {
		var err error
		constraint, admitted, err = projectPhysicalOrderConstraint(constraint, selected.SurvivingRows(), pendingInstalls)
		if err != nil || !admitted {
			return reconcile.RelationOrderDecision{}, false, err
		}
	}
	decision, err := reconcile.NewSelectedRelationOrderDecision(reconcile.SelectedRelationOrderDecisionInput{
		Target:          selectedTarget,
		Scope:           capability.Scope(),
		Constraint:      constraint,
		Selected:        selected,
		PendingInstalls: pendingInstalls,
	})
	return decision, err == nil, err
}

func projectPhysicalOrderConstraint(
	constraint hostrelation.RelationOrderConstraint,
	rows []observerelation.ObservedRelationRow,
	pendingInstalls []topology.SubjectID,
) (hostrelation.RelationOrderConstraint, bool, error) {
	selected := make(map[topology.SubjectID]struct{})
	for _, row := range rows {
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
