package apply

import (
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
)

func requiresCarrierHandoffPhase(result reconcile.Result) bool {
	if _, present := result.CarrierHandoff(); present {
		return true
	}
	for _, absence := range result.CarrierAbsences() {
		if absence.Desired() == carrierabsence.DesiredReplaced {
			return true
		}
	}
	return false
}
