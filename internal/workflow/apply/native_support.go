package apply

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type nativeSupportReason uint8

const (
	nativeSupportRoutes nativeSupportReason = iota + 1
	nativeSupportOrder
	nativeSupportCarrierState
)

type nativeSupportError struct{ reason nativeSupportReason }

func (err nativeSupportError) Error() string { return err.reason.detail() }

func (reason nativeSupportReason) detail() string {
	switch reason {
	case nativeSupportRoutes:
		return "this extension or host-registration operation can rewrite settings containing managed keys and does not yet support key-protected execution"
	case nativeSupportOrder:
		return "key-mode package-order changes require ownership of the same Pi settings layer"
	case nativeSupportCarrierState:
		return "extension ownership settlement is not yet supported while settings keys are managed"
	default:
		return "the selected settings-key execution is not supported"
	}
}

func checkNativeCommandSupport(assessment readiness.Assessment) error {
	result := assessment.Reconciliation
	if len(assessment.NativeSources) == 0 && !assessment.CurrentState.ContainsHostConfig() && !result.ContainsHostConfig() {
		return nil
	}
	if err := rejectBlockedRelationActions(result); err != nil {
		return err
	}
	if err := rejectBlockedRelationOrders(result); err != nil {
		return err
	}
	if err := rejectBlockedCarrierAdoptions(result); err != nil {
		return err
	}
	if err := rejectBlockedCarrierAbsences(result.CarrierAbsences()); err != nil {
		return err
	}
	for _, relation := range result.Relations() {
		if relation.Execution() != reconcile.ExecutionNoMutation {
			if !relation.InvokesHostRoute() || !nativeInvocationCarrierSupported(relation.CarrierIdentity().Carrier().Key()) {
				return nativeSupportError{nativeSupportRoutes}
			}
			if _, pin := relation.PinTransition(); pin {
				return nativeSupportError{nativeSupportRoutes}
			}
		}
	}
	for _, delegate := range result.Delegates() {
		if delegate.Disposition() != reconcile.DelegateSkipped {
			return nativeSupportError{nativeSupportRoutes}
		}
	}
	for _, absence := range result.CarrierAbsences() {
		if absence.BlocksOrdinaryApply() || absence.InvokesHostRoute() || absence.MutatesDirectProjection() || absence.RetiresClaim() {
			if nativeInvocationCarrierSupported(absence.Claim().Identity().Carrier().Key()) && !absence.MutatesDirectProjection() {
				continue
			}
			if !absence.InvokesHostRoute() && !absence.MutatesDirectProjection() {
				return nativeSupportError{nativeSupportCarrierState}
			}
			return nativeSupportError{nativeSupportRoutes}
		}
	}
	for _, adoption := range result.CarrierAdoptions() {
		if adoption.BlocksOrdinaryApply() || adoption.StateOnly() {
			if !nativeAdoptionCompletedByPending(assessment.CurrentState, result.Relations(), adoption) {
				return nativeSupportError{nativeSupportCarrierState}
			}
		}
	}
	for _, order := range result.RelationOrders() {
		if order.RequiresMutation() {
			if order.Target() != target.TargetPi {
				return nativeSupportError{nativeSupportOrder}
			}
		}
	}
	return nil
}

func nativeInvocationCarrierSupported(key desiredextension.CarrierKey) bool {
	switch key.Carrier() {
	case desiredextension.CarrierPiPackage:
		return key.Target() == target.TargetPi
	case desiredextension.CarrierCodexPlugin:
		return key.Target() == target.TargetCodex && key.Scope() == target.ScopeGlobal
	default:
		return false
	}
}

func (reason nativeSupportReason) repairInstruction() string {
	switch reason {
	case nativeSupportRoutes:
		return "defer this extension change and retain its previous declaration; splitting off a key-value update does not bypass managed-key ownership"
	case nativeSupportOrder:
		return "apply only same-layer Pi package-order changes, or defer this order change"
	case nativeSupportCarrierState:
		return "retain the existing extension declarations and defer this ownership change while settings keys are managed"
	default:
		return ""
	}
}

func nativeAdoptionCompletedByPending(current durable.Snapshot, actions []reconcile.RelationAction, adoption carrieradoption.Action) bool {
	if adoption.Result() != carrieradoption.ResultPresentUnclaimed && adoption.Result() != carrieradoption.ResultEligibleExactRelation {
		return false
	}
	for _, action := range actions {
		if !action.CarrierIdentity().ExactEqual(adoption.CarrierIdentity()) || !action.RouteRequest().Equal(adoption.AcquisitionRequest()) {
			continue
		}
		pending, _, matched := execute.MatchPendingCarrierInstallCompletion(current, action, action.Scope())
		if matched && pending.Owner().ExactEqual(adoption.CurrentOwner()) {
			return true
		}
	}
	return false
}
