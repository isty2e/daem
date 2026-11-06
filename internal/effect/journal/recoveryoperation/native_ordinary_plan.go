package recoveryoperation

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

// OrdinaryActions projects only the rollback phase selected by the complete
// operation. Committed After-state recovery never replays host restoration.
func (plan NativeRecoveryPlan) OrdinaryActions() []recovery.Action {
	if plan.Blocked() || plan.assessment.Classification() != recovery.ClassificationNeedsRollback {
		return nil
	}
	return plan.assessment.OrdinaryActions()
}

func (plan NativeRecoveryPlan) OrdinaryProvisionalAcquires() ([]ownership.ProvisionalAcquireIntent, error) {
	return plan.journal.ProvisionalAcquires()
}

func (plan NativeRecoveryPlan) ActiveJournalAuthority() journalinventory.ActiveJournalAuthority {
	return plan.active
}

func (operation *NativeRecoveryOperation) OrdinaryPhysicalAuthorityRequests() ([]mutation.PhysicalAuthorityRequest, error) {
	if operation == nil || operation.closed {
		return nil, fmt.Errorf("live recovery operation and independent resolver are required")
	}
	entries, err := operation.plan.journal.RecordedEntries()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	if operation.resolver == nil {
		return nil, fmt.Errorf("ordinary recovery requires its independent resolver")
	}
	var requests []mutation.PhysicalAuthorityRequest
	appendRequest := func(destination output.Destination, scope, selectedTarget string) error {
		path, err := operation.resolver(destination)
		if err != nil {
			return err
		}
		request := mutation.PhysicalAuthorityRequest{Path: path, Target: selectedTarget, Scope: scope}
		if !slices.Contains(requests, request) {
			requests = append(requests, request)
		}
		return nil
	}
	for _, entry := range entries {
		scope, destination, err := entry.WriterAddress()
		if err != nil {
			return nil, err
		}
		targets, err := entry.ConsumerTargets()
		if err != nil {
			return nil, err
		}
		for _, selectedTarget := range targets {
			if err := appendRequest(destination, string(scope), string(selectedTarget)); err != nil {
				return nil, err
			}
		}
	}
	return requests, nil
}

func (operation *NativeRecoveryOperation) Owner() stateauthority.Authority {
	if operation == nil || operation.closed {
		return stateauthority.Authority{}
	}
	return operation.plan.journal.Owner()
}

// GuardedOrdinaryActions supplies every ordinary address in the observed
// operation, including current entries and metadata-only finalization.
func (plan NativeRecoveryPlan) GuardedOrdinaryActions() []recovery.Action {
	return plan.assessment.OrdinaryActions()
}
