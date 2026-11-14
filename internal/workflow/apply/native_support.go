package apply

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type nativeSupportReason uint8

const (
	nativeSupportRoutes nativeSupportReason = iota + 1
	nativeSupportOrder
	nativeSupportCarrierState
	nativeSupportNamespace
	nativeSupportWholeRewrite
)

type nativeSupportError struct{ reason nativeSupportReason }

func (err nativeSupportError) Error() string { return err.reason.detail() }

func (reason nativeSupportReason) detail() string {
	switch reason {
	case nativeSupportRoutes:
		return "this extension or host-registration operation can rewrite settings containing managed keys and does not yet support key-protected execution"
	case nativeSupportOrder:
		return "settings-key order changes require a reserved native actor or a physically disjoint direct actor"
	case nativeSupportCarrierState:
		return "extension ownership settlement is not yet supported while settings keys are managed"
	case nativeSupportNamespace:
		return "selected settings overlap the plugin host's private namespace without a supported rewrite contract"
	case nativeSupportWholeRewrite:
		return "whole-file ownership overlaps settings rewritten by an extension command"
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
			if !relation.InvokesHostRoute() || !nativeInvocationCarrierSupported(relation.CarrierIdentity().Carrier().Key(), lock.OperationInstall) {
				return nativeSupportError{nativeSupportRoutes}
			}
		}
	}
	for _, absence := range result.CarrierAbsences() {
		if absence.BlocksOrdinaryApply() || absence.InvokesHostRoute() || absence.MutatesDirectProjection() || absence.RetiresClaim() {
			profile, err := relationhost.NativeSettingsProfileFor(absence.Claim().Identity().Carrier().Key(), lock.OperationRemove)
			if err == nil && (!absence.MutatesDirectProjection() || profile.RequireActuation(lock.ActuationDirectProjection) == nil) {
				continue
			}
			if !absence.InvokesHostRoute() && !absence.MutatesDirectProjection() {
				return nativeSupportError{nativeSupportCarrierState}
			}
			return nativeSupportError{nativeSupportRoutes}
		}
	}
	for _, adoption := range result.CarrierAdoptions() {
		if adoption.StateOnly() {
			if !nativeAdoptionCompletedByPending(assessment.CurrentState, result.Relations(), adoption) {
				return nativeSupportError{nativeSupportCarrierState}
			}
		}
	}
	return nil
}

func nativeDelegatesScheduled(actions []reconcile.DelegateAction) bool {
	for _, action := range actions {
		if action.SchedulesAttempt() {
			return true
		}
	}
	return false
}

func nativeInvocationCarrierSupported(key desiredextension.CarrierKey, operation lock.OperationKind) bool {
	profile, err := relationhost.NativeSettingsProfileFor(key, operation)
	return err == nil && profile.RequireActuation(lock.ActuationDelegatedHostRoute) == nil
}

func (reason nativeSupportReason) repairInstruction() string {
	switch reason {
	case nativeSupportRoutes:
		return "defer this extension change and retain its previous declaration; splitting off a key-value update does not bypass managed-key ownership"
	case nativeSupportOrder:
		return "inspect a fresh order preview; defer order work that cannot bind to its reserved native or disjoint direct actor"
	case nativeSupportCarrierState:
		return "retain the existing extension declarations and defer this ownership change while settings keys are managed"
	case nativeSupportNamespace:
		return "inspect resolved configuration-root aliases; this overlap requires an explicit rewrite profile before apply"
	case nativeSupportWholeRewrite:
		return "use key ownership for the shared settings file, or choose a separate dedicated file"
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
		pending, _, matched := effectpreparation.MatchPendingCarrierInstallCompletion(current, action)
		if matched && pending.Owner().ExactEqual(adoption.CurrentOwner()) {
			return true
		}
	}
	return false
}
