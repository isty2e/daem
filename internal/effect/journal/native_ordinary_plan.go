package journal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func (ordinary nativeOrdinaryAssessment) observationFingerprint() string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%t\x00%t\x00%t\x00", ordinary.entries.MatchesBefore(), ordinary.entries.MatchesExpected(), ordinary.entries.Recoverable())
	for _, path := range ordinary.paths {
		mode := "absent"
		if path.PathMode != nil {
			mode = fmt.Sprintf("%04o", *path.PathMode)
		}
		path.PathMode = nil
		fmt.Fprintf(hash, "%#v\x00%s\x00", path, mode)
	}
	for _, backup := range ordinary.backups {
		fmt.Fprintf(hash, "%#v\x00", backup)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// OrdinaryActions projects only the rollback phase selected by the complete
// operation. Committed After-state recovery never replays host restoration.
func (plan NativeRecoveryPlan) OrdinaryActions() []recovery.Action {
	if plan.Blocked() || plan.assessment.disposition != nativeDocumentsRollback {
		return nil
	}
	return plan.assessment.ordinary.entries.Actions()
}

func (plan NativeRecoveryPlan) OrdinaryProvisionalAcquires() ([]ownership.ProvisionalAcquireIntent, error) {
	return plan.journal.ProvisionalAcquires()
}

func (plan NativeRecoveryPlan) ActiveJournalAuthority() ActiveJournalAuthority {
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
	return plan.assessment.ordinary.entries.Actions()
}
