package apply

import (
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
)

// CarrierHandoffProgress records observed retirement progress independently
// from the plan-time conditional remainder.
type CarrierHandoffProgress struct {
	RetiredClaims []durablecarrier.ManagedCarrierClaim
	Requalified   bool
	Complete      bool
}

func carrierHandoffProgressFor(phase carrierHandoffPhaseResult, actions []carrierabsence.Action, requalified bool, commitErr error) CarrierHandoffProgress {
	progress := CarrierHandoffProgress{Requalified: requalified}
	if phase.retired.StatePath == "" {
		return progress
	}
	for _, action := range actions {
		claim := action.Claim()
		remaining := globalCarrierMembershipUnconfirmed(commitErr, claim)
		for _, current := range append(phase.retired.State.ManagedCarrierClaims(), phase.retired.GlobalCarrierClaims.Claims()...) {
			remaining = remaining || current.ExactEqual(claim)
		}
		for _, pending := range phase.retired.State.PendingCarrierRemovals() {
			remaining = remaining || pending.Claim().ExactEqual(claim)
		}
		if !remaining {
			progress.RetiredClaims = append(progress.RetiredClaims, claim)
		}
	}
	return progress
}

func cloneCarrierHandoffProgress(progress *CarrierHandoffProgress) *CarrierHandoffProgress {
	if progress == nil {
		return nil
	}
	copy := *progress
	copy.RetiredClaims = append([]durablecarrier.ManagedCarrierClaim(nil), progress.RetiredClaims...)
	return &copy
}
