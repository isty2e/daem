package recover

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
)

// ExecutionPhase is the public projection of post-execution durable authority
// and command outcome. The model stores those axes separately.
type ExecutionPhase string

const (
	ExecutionPhaseActiveAuthorityRetained  ExecutionPhase = "active_authority_retained"
	ExecutionPhaseCleanupAuthorityRetained ExecutionPhase = "cleanup_authority_retained"
	ExecutionPhaseAuthorityRetired         ExecutionPhase = "authority_retired"
	ExecutionPhaseCompleted                ExecutionPhase = "completed"
	ExecutionPhaseAuthorityUnknown         ExecutionPhase = "authority_unknown"
)

type durableAuthorityState uint8

const (
	durableAuthorityInvalid durableAuthorityState = iota
	durableAuthorityActive
	durableAuthorityCleanup
	durableAuthorityNone
	durableAuthorityUnknown
)

// ExecutionResult is a path-neutral post-execution fact. A retained state owns
// a fresh disclosure; terminal and unknown states deliberately carry no stale
// plan or action authority.
type ExecutionResult struct {
	authorityState     durableAuthorityState
	disclosure         journal.RecoverablePlan
	operationID        string
	executionSucceeded bool
	fileSetFence       fileset.FileSetFenceObservation
}

type terminalExecutionFailure struct {
	cause error
}

func (failure *terminalExecutionFailure) Error() string {
	if journal.IsRetirementFinalizedWithGCResidue(failure.cause) {
		return "recovery authority retired; hidden GC cleanup did not complete successfully; no recovery action remains"
	}
	return "recovery authority retired; post-retirement validation did not complete successfully; no recovery action remains"
}

func (failure *terminalExecutionFailure) Unwrap() error { return failure.cause }

type unknownAuthorityFailure struct {
	cause error
}

func (failure *unknownAuthorityFailure) Error() string {
	return "recovery write failed and current durable authority could not be classified; preserve recovery artifacts and inspect again"
}

func (failure *unknownAuthorityFailure) Unwrap() error { return failure.cause }

func retainedExecutionResult(selection journal.RecoverablePlan) (ExecutionResult, error) {
	if selection == nil {
		return ExecutionResult{}, fmt.Errorf("recovery selection is uninitialized")
	}
	result := ExecutionResult{disclosure: selection.Clone()}
	if active, ok := journal.ActiveRecoveryPlan(selection); ok {
		result.authorityState = durableAuthorityActive
		result.operationID = active.OperationID()
		return result, nil
	}
	if cleanup, ok := journal.JournalCleanupPlan(selection); ok {
		result.authorityState = durableAuthorityCleanup
		result.operationID = cleanup.Authority().OperationID()
		return result, nil
	}
	return ExecutionResult{}, fmt.Errorf("recovery selection is uninitialized")
}

func retiredExecutionResult(operationID string, succeeded bool) ExecutionResult {
	return ExecutionResult{
		authorityState:     durableAuthorityNone,
		operationID:        operationID,
		executionSucceeded: succeeded,
	}
}

func unknownExecutionResult(operationID string) ExecutionResult {
	return ExecutionResult{
		authorityState: durableAuthorityUnknown,
		operationID:    operationID,
	}
}

func (result ExecutionResult) withExecutionFailure() ExecutionResult {
	result.executionSucceeded = false
	return result
}

func (result ExecutionResult) withFileSetFence(kind fileset.FileSetFenceKind) ExecutionResult {
	return result.withFileSetFenceObservation(fileset.KnownFileSetFence(kind))
}

func (result ExecutionResult) withFileSetFenceObservation(
	observation fileset.FileSetFenceObservation,
) ExecutionResult {
	result.fileSetFence = observation
	return result
}

// FileSetFenceObservation returns the fresh terminal file-set axis independently
// of journal authority retirement.
func (result ExecutionResult) FileSetFenceObservation() fileset.FileSetFenceObservation {
	return result.fileSetFence
}

// HasNonClearFileSetObservation reports a known non-clear or observed-unknown
// terminal file-set fact.
func (result ExecutionResult) HasNonClearFileSetObservation() bool {
	return result.fileSetFence.Observed() &&
		(!result.fileSetFence.Known() || result.fileSetFence.Kind() != fileset.FileSetFenceClear)
}

// Phase returns the public post-execution lifecycle projection.
func (result ExecutionResult) Phase() ExecutionPhase {
	switch result.authorityState {
	case durableAuthorityActive:
		return ExecutionPhaseActiveAuthorityRetained
	case durableAuthorityCleanup:
		return ExecutionPhaseCleanupAuthorityRetained
	case durableAuthorityNone:
		if result.executionSucceeded {
			return ExecutionPhaseCompleted
		}
		return ExecutionPhaseAuthorityRetired
	case durableAuthorityUnknown:
		return ExecutionPhaseAuthorityUnknown
	default:
		return ""
	}
}

// AuthorityKind returns the freshly observed retained authority kind.
func (result ExecutionResult) AuthorityKind() journal.RecoveryAuthorityKind {
	switch result.authorityState {
	case durableAuthorityActive:
		return journal.RecoveryAuthorityActiveJournal
	case durableAuthorityCleanup:
		return journal.RecoveryAuthorityJournalCleanup
	default:
		return ""
	}
}

// OperationID returns the path-neutral durable operation identity.
func (result ExecutionResult) OperationID() string { return result.operationID }

// CurrentDisclosure returns a defensive fresh disclosure only while exact
// retry authority remains classified.
func (result ExecutionResult) CurrentDisclosure() (journal.RecoverablePlan, bool) {
	if !result.AuthorityRetained() || result.disclosure == nil {
		return nil, false
	}
	return result.disclosure.Clone(), true
}

// AuthorityRetained reports whether a freshly classified retry authority is
// available.
func (result ExecutionResult) AuthorityRetained() bool {
	return result.authorityState == durableAuthorityActive ||
		result.authorityState == durableAuthorityCleanup
}

// SemanticError preserves actionable causes only when exact current authority
// remains. Terminal and unknown states expose path-neutral lifecycle facts.
func (result ExecutionResult) SemanticError(cause error) error {
	if cause == nil {
		return nil
	}
	switch result.authorityState {
	case durableAuthorityUnknown:
		var existing *unknownAuthorityFailure
		if errors.As(cause, &existing) {
			return cause
		}
		return &unknownAuthorityFailure{cause: cause}
	case durableAuthorityNone:
		var existing *terminalExecutionFailure
		if errors.As(cause, &existing) {
			return cause
		}
		return &terminalExecutionFailure{cause: cause}
	case durableAuthorityCleanup:
		cleanup, ok := journal.JournalCleanupPlan(result.disclosure)
		if !ok {
			return &unknownAuthorityFailure{cause: cause}
		}
		return journal.WrapCleanupFailure(cleanup.Action(), cause)
	default:
		return cause
	}
}
