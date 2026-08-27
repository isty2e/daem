package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

// ErrInterruptedApply reports that RecoveryDir contains an active or prepared
// apply journal. daem recover can produce a plan for that authority.
var ErrInterruptedApply = errors.New("interrupted apply operation found")

// ErrIncompleteJournalCleanup reports that RecoveryDir contains retained or
// finalizing journal cleanup. daem recover can produce a plan for that authority.
var ErrIncompleteJournalCleanup = errors.New("journal cleanup is incomplete")

// InterruptionKind is the closed journal readiness axis used by workflow
// recovery barriers.
type InterruptionKind string

const (
	InterruptionClear       InterruptionKind = ""
	InterruptionActiveApply InterruptionKind = "active_apply"
	InterruptionCleanupOnly InterruptionKind = "cleanup_only"
	InterruptionInvalid     InterruptionKind = "invalid"
)

// InterruptionKindOf returns the most specific journal authority preserved by
// err. Conflicting active and cleanup sentinels are invalid rather than
// arbitrarily assigned to either recovery path.
func InterruptionKindOf(err error) InterruptionKind {
	if err == nil {
		return InterruptionClear
	}
	active := errors.Is(err, ErrInterruptedApply)
	cleanup := errors.Is(err, ErrIncompleteJournalCleanup)
	switch {
	case active && cleanup:
		return InterruptionInvalid
	case active:
		return InterruptionActiveApply
	case cleanup:
		return InterruptionCleanupOnly
	default:
		return InterruptionClear
	}
}

// RequireNoInterruptedApply applies the canonical recovery-root readiness
// policy through the production filesystem and state codecs.
func RequireNoInterruptedApply(ctx context.Context, recoveryRoot string) error {
	return ensureNoActive(ctx, recoveryRoot, inventoryOptions{
		Filesystem: storagecommit.Adapter{},
		StateCodec: statefile.Codec{},
	})
}

// ensureNoActive permits mutation only when the canonical recovery inventory
// is clean or contains inert finalized GC residue.
func ensureNoActive(
	ctx context.Context,
	recoveryRoot string,
	options inventoryOptions,
) error {
	inventory, err := loadRecoveryRootInventory(ctx, recoveryRoot, options)
	if err != nil {
		return err
	}
	switch inventory.decision.State() {
	case retirement.StateClean, retirement.StateFinalized:
		return nil
	case retirement.StateActive, retirement.StatePrepared:
		return fmt.Errorf("%w; run: daem recover --dry-run", ErrInterruptedApply)
	case retirement.StateRetained,
		retirement.StateFinalizing:
		return fmt.Errorf("%w; run: daem recover --dry-run", ErrIncompleteJournalCleanup)
	case retirement.StateBlocked:
		return fmt.Errorf("recovery inventory is blocked: %s", inventory.decision.Detail())
	default:
		return fmt.Errorf("recovery inventory classification is invalid")
	}
}

func isSafeRecoveryOperationID(value string) bool {
	return retirement.ValidateOperationID(value) == nil
}

// CleanupFailurePhase identifies the semantic phase in which one cleanup-only
// recovery action failed.
type CleanupFailurePhase string

const (
	CleanupFailurePhaseExecution         CleanupFailurePhase = "execution"
	CleanupFailurePhaseGarbageCollection CleanupFailurePhase = "garbage_collection"
)

type finalizedGCResidueError struct {
	cause error
}

func (failure *finalizedGCResidueError) Error() string {
	return "journal retirement committed; hidden GC cleanup did not complete successfully; no recovery action remains"
}

func (failure *finalizedGCResidueError) Unwrap() error {
	return failure.cause
}

// IsRetirementFinalizedWithGCResidue reports whether semantic journal
// retirement completed before a later GC step failed.
func IsRetirementFinalizedWithGCResidue(err error) bool {
	var failure *finalizedGCResidueError
	return errors.As(err, &failure)
}

func finalizedWithGCResidue(cause error) error {
	if cause == nil {
		return nil
	}
	return &finalizedGCResidueError{cause: cause}
}

// CleanupFailure is the path-neutral result of one failed cleanup-only
// recovery action. Cause remains available for internal inspection but is not
// included in Error.
type CleanupFailure struct {
	action retirement.CleanupActionKind
	phase  CleanupFailurePhase
	cause  error
}

// Action returns the exact cleanup action selected by the recovery plan.
func (failure *CleanupFailure) Action() retirement.CleanupActionKind {
	if failure == nil {
		return ""
	}
	return failure.action
}

// Phase returns the semantic phase in which execution failed.
func (failure *CleanupFailure) Phase() CleanupFailurePhase {
	if failure == nil {
		return ""
	}
	return failure.phase
}

func (failure *CleanupFailure) Error() string {
	if failure == nil {
		return "journal cleanup failed"
	}
	switch failure.phase {
	case CleanupFailurePhaseGarbageCollection:
		return fmt.Sprintf(
			"journal cleanup incomplete: phase=%s action=%s; semantic retirement is committed and no recovery action remains",
			failure.phase,
			failure.action,
		)
	default:
		return fmt.Sprintf(
			"journal cleanup failed: phase=%s action=%s",
			failure.phase,
			failure.action,
		)
	}
}

func (failure *CleanupFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

// WrapCleanupFailure normalizes one cleanup-only execution error without
// exposing its boundary-specific cause.
func WrapCleanupFailure(
	action retirement.CleanupActionKind,
	err error,
) error {
	if err == nil {
		return nil
	}
	var existing *CleanupFailure
	if errors.As(err, &existing) && existing.Action() == action {
		return err
	}
	phase := CleanupFailurePhaseExecution
	if IsRetirementFinalizedWithGCResidue(err) {
		phase = CleanupFailurePhaseGarbageCollection
	}
	return &CleanupFailure{
		action: action,
		phase:  phase,
		cause:  err,
	}
}
