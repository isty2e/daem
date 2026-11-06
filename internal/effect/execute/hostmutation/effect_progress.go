package hostmutation

import (
	"errors"

	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

// EffectProgress records what one host action establishes before compensation.
type EffectProgress uint8

const (
	EffectNotStarted EffectProgress = iota
	EffectExpectedAfter
	EffectIndeterminate
)

func (progress EffectProgress) RollbackEligible() bool { return progress == EffectExpectedAfter }
func (progress EffectProgress) RequiresRecovery() bool { return progress == EffectIndeterminate }

func ProgressAfterMutationError(err error) EffectProgress {
	if mutationfs.HasIndeterminateEffect(err) {
		return EffectIndeterminate
	}
	var removalFailure *removaloperation.CommitError
	if errors.As(err, &removalFailure) {
		return progressAfterCommitOutcome(removalFailure.Outcome(), err)
	}
	kind, classified := mutationfs.FailureKindOf(err)
	if !classified {
		return EffectIndeterminate
	}
	switch kind {
	case mutationfs.FailureUncommitted, mutationfs.FailureUnsupportedGuarantee:
		return EffectNotStarted
	case mutationfs.FailureIndeterminateCommit, mutationfs.FailureRetainedResidue:
		return EffectIndeterminate
	default:
		return EffectIndeterminate
	}
}

func progressAfterCommitOutcome(outcome mutationfs.CommitOutcome, err error) EffectProgress {
	if err == nil {
		return EffectExpectedAfter
	}
	switch outcome.State() {
	case mutationfs.CommitOutcomeUncommitted:
		return EffectNotStarted
	case mutationfs.CommitOutcomeIndeterminate, mutationfs.CommitOutcomeRetainedRecoverable,
		mutationfs.CommitOutcomeComplete:
		return EffectIndeterminate
	default:
		// A rooted removal must carry a canonical storage outcome. An unknown
		// outcome is indeterminate; reclassifying its wrapped error would recurse.
		return EffectIndeterminate
	}
}
