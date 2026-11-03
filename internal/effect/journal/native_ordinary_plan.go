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
	return canonicalProvisionalAcquireIntents(plan.journal.record.ProvisionalAcquires)
}

func (plan NativeRecoveryPlan) ActiveJournalAuthority() ActiveJournalAuthority {
	return plan.active
}

func (operation *NativeRecoveryOperation) OrdinaryPhysicalAuthorityRequests() ([]mutation.PhysicalAuthorityRequest, error) {
	if operation == nil || operation.closed {
		return nil, fmt.Errorf("live recovery operation and independent resolver are required")
	}
	if len(operation.plan.journal.record.Entries) == 0 {
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
	for _, entry := range operation.plan.journal.record.Entries {
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return nil, err
		}
		targets := entry.Targets
		if len(targets) == 0 {
			targets = []string{entry.Target}
		}
		for _, selectedTarget := range targets {
			if err := appendRequest(destination, entry.Scope, selectedTarget); err != nil {
				return nil, err
			}
		}
	}
	return requests, nil
}

func (operation *NativeRecoveryOperation) Owner() stateauthority.Authority {
	return operation.plan.journal.owner
}

// GuardedOrdinaryActions supplies every ordinary address in the observed
// operation, including current entries and metadata-only finalization.
func (plan NativeRecoveryPlan) GuardedOrdinaryActions() []recovery.Action {
	return plan.assessment.ordinary.entries.Actions()
}
