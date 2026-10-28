package apply

import (
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type nativeSupportReason uint8

const (
	nativeSupportFiles nativeSupportReason = iota + 1
	nativeSupportRoutes
	nativeSupportOrder
	nativeSupportCarrierState
)

type nativeSupportError struct{ reason nativeSupportReason }

func (err nativeSupportError) Error() string { return err.reason.detail() }

func (reason nativeSupportReason) detail() string {
	switch reason {
	case nativeSupportFiles:
		return "settings keys and ordinary file updates cannot yet be applied together"
	case nativeSupportRoutes:
		return "settings keys and extension or host-registration changes cannot yet be applied together"
	case nativeSupportOrder:
		return "settings keys and package-order changes cannot yet be applied together"
	case nativeSupportCarrierState:
		return "settings keys and extension ownership settlement cannot yet be applied together"
	default:
		return "the selected settings-key execution is not supported"
	}
}

func checkNativeCommandSupport(assessment readiness.Assessment) error {
	result := assessment.Reconciliation
	if len(assessment.NativeSources) == 0 && !assessment.CurrentState.ContainsHostConfig() && !result.ContainsHostConfig() {
		return nil
	}
	if len(result.MutatingManagedPaths()) != 0 {
		return nativeSupportError{nativeSupportFiles}
	}
	for _, relation := range result.Relations() {
		if relation.Execution() != reconcile.ExecutionNoMutation {
			return nativeSupportError{nativeSupportRoutes}
		}
	}
	for _, delegate := range result.Delegates() {
		if delegate.Disposition() != reconcile.DelegateSkipped {
			return nativeSupportError{nativeSupportRoutes}
		}
	}
	if len(result.CarrierAbsences()) != 0 || len(result.CarrierAdoptions()) != 0 {
		return nativeSupportError{nativeSupportRoutes}
	}
	for _, order := range result.RelationOrders() {
		if !order.IsExact() {
			return nativeSupportError{nativeSupportOrder}
		}
	}
	if _, changed, err := assessment.CurrentState.WithConvergedGlobalCarrierClaims(assessment.GlobalCarrierClaims); err != nil {
		return err
	} else if changed {
		return nativeSupportError{nativeSupportCarrierState}
	}
	return nil
}
