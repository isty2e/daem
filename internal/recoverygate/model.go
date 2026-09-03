package recoverygate

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// State preserves each observed recovery axis without conflating an
// unclassified observation with an axis that was not observed.
type State struct {
	journal         journal.InterruptionKind
	fileSet         fileset.FileSetFenceKind
	journalObserved bool
	journalKnown    bool
	fileSetObserved bool
	fileSetKnown    bool
}

// Journal returns the classified journal interruption axis. Callers that need
// to distinguish clear, unknown, and unobserved must also inspect the evidence
// methods below.
func (state State) Journal() journal.InterruptionKind { return state.journal }

// JournalObserved reports whether the journal axis participated in this fact.
func (state State) JournalObserved() bool { return state.journalObserved }

// JournalKnown reports whether an observed journal axis was classified.
func (state State) JournalKnown() bool { return state.journalObserved && state.journalKnown }

// FileSet returns the classified file-set fence axis.
func (state State) FileSet() fileset.FileSetFenceKind { return state.fileSet }

// FileSetObserved reports whether the file-set axis participated in this fact.
func (state State) FileSetObserved() bool { return state.fileSetObserved }

// FileSetKnown reports whether an observed file-set axis was classified.
func (state State) FileSetKnown() bool { return state.fileSetObserved && state.fileSetKnown }

// Observed reports whether either recovery axis is represented.
func (state State) Observed() bool { return state.journalObserved || state.fileSetObserved }

// HasContinuingFileSetFence reports file-set states that remain after journal
// recovery but do not invalidate StateDir access or transaction evidence.
func (state State) HasContinuingFileSetFence() bool {
	if !state.FileSetKnown() {
		return false
	}
	switch state.fileSet {
	case fileset.FileSetFencePublishedTransaction,
		fileset.FileSetFenceAbandonedResidue,
		fileset.FileSetFenceCensusLimit:
		return true
	default:
		return false
	}
}

// blocksJournalRecovery reports file-set states that cannot safely coexist
// with host or cleanup recovery effects.
func (state State) blocksJournalRecovery() bool {
	return state.FileSetKnown() &&
		(state.fileSet == fileset.FileSetFenceAccessUnprovable ||
			state.fileSet == fileset.FileSetFenceInvalidEvidence)
}

func jointState(journalErr error, fileSetErr error) State {
	state := State{journalObserved: true, fileSetObserved: true}
	if journalErr == nil {
		state.journalKnown = true
	} else {
		state.journal = journal.InterruptionKindOf(journalErr)
		state.journalKnown = state.journal != journal.InterruptionClear
	}
	if fileSetErr == nil {
		state.fileSetKnown = true
	} else {
		state.fileSet = fileset.FileSetFenceKindOf(fileSetErr)
		state.fileSetKnown = state.fileSet != fileset.FileSetFenceClear
	}
	return state
}

func standaloneState(err error) State {
	if err == nil {
		return State{}
	}
	state := State{}
	if kind := journal.InterruptionKindOf(err); kind != journal.InterruptionClear {
		state.journal = kind
		state.journalObserved = true
		state.journalKnown = true
	}
	if kind := fileset.FileSetFenceKindOf(err); kind != fileset.FileSetFenceClear {
		state.fileSet = kind
		state.fileSetObserved = true
		state.fileSetKnown = true
	}
	return state
}

// jointError preserves both observed causes while projecting cancellation and
// access-first precedence at the workflow boundary.
type jointError struct {
	state      State
	journalErr error
	fileSetErr error
}

func (err *jointError) observedState() State {
	if err == nil {
		return State{}
	}
	return err.state
}

func (err *jointError) Error() string {
	if err == nil {
		return ""
	}
	if err.state.blocksJournalRecovery() && err.journalErr != nil {
		if err.state.journal == journal.InterruptionActiveApply ||
			err.state.journal == journal.InterruptionCleanupOnly {
			return fmt.Sprintf(
				"%s; observed journal authority cannot be acted on until the state directory boundary is restored: %s",
				err.fileSetErr,
				err.journalErr,
			)
		}
		return fmt.Sprintf(
			"%s; journal observation also failed: %s",
			err.fileSetErr,
			err.journalErr,
		)
	}
	if err.state.HasContinuingFileSetFence() {
		switch err.state.journal {
		case journal.InterruptionActiveApply:
			return fmt.Sprintf(
				"%s; continuing file-set fence: %s; daem recover can process the journal, but it does not clear the continuing file-set fence",
				err.journalErr,
				err.fileSetErr,
			)
		case journal.InterruptionCleanupOnly:
			return fmt.Sprintf(
				"%s; continuing file-set fence: %s; daem recover can finish journal cleanup, but it does not clear the continuing file-set fence",
				err.journalErr,
				err.fileSetErr,
			)
		}
	}
	if err.journalErr != nil && err.fileSetErr != nil {
		return fmt.Sprintf("%s; %s", err.journalErr, err.fileSetErr)
	}
	if err.journalErr != nil {
		return err.journalErr.Error()
	}
	if err.fileSetErr != nil {
		return err.fileSetErr.Error()
	}
	return ""
}

func (err *jointError) Unwrap() []error {
	if err == nil {
		return nil
	}
	causes := make([]error, 0, 2)
	if err.journalErr != nil {
		causes = append(causes, err.journalErr)
	}
	if err.fileSetErr != nil {
		causes = append(causes, err.fileSetErr)
	}
	return causes
}

// Combine constructs one cancellation-safe joint barrier error from already
// observed peer facts. Nil is an observed clear axis, while an unclassified
// non-nil error remains observed but unknown.
func Combine(journalErr error, fileSetErr error) error {
	if cancellation := cancellationCause(journalErr, fileSetErr); cancellation != nil {
		return cancellation
	}
	if journalErr == nil && fileSetErr == nil {
		return nil
	}
	return &jointError{
		state:      jointState(journalErr, fileSetErr),
		journalErr: journalErr,
		fileSetErr: fileSetErr,
	}
}

// Observe reads the independent RecoveryDir journal and StateDir file-set facts
// without allowing a later observation to hide caller cancellation.
func Observe(ctx context.Context, paths daempaths.Paths) error {
	if ctx == nil {
		return fmt.Errorf("recovery barrier context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stateDir, stateDirErr := CaptureStateDir(ctx, paths.StateDir)
	if err := ctx.Err(); err != nil {
		return err
	}
	journalErr := journal.RequireNoInterruptedApply(ctx, paths.RecoveryDir)
	if err := ctx.Err(); err != nil {
		return err
	}
	fileSetErr := stateDirErr
	if fileSetErr == nil {
		fileSetErr = stateDir.RequireClear(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return Combine(journalErr, fileSetErr)
}

// RequireClear requires both peer recovery barriers to be clear.
func RequireClear(ctx context.Context, paths daempaths.Paths) error {
	return Observe(ctx, paths)
}

// RequireFileSetClear observes only the StateDir file-set fence. It does not
// inspect RecoveryDir journals. Lock planning, init planning, and authoring
// dry-run retain this compatibility posture; joint journal and file-set
// refusal uses RequireClear or EffectAuthority.
func RequireFileSetClear(ctx context.Context, stateDir string) error {
	if err := requireBarrierContext(ctx); err != nil {
		return err
	}
	authority, err := CaptureStateDir(ctx, stateDir)
	if err != nil {
		return err
	}
	return authority.RequireClear(ctx)
}

// StateOf reconstructs the closed joint state through arbitrary error wrapping.
func StateOf(err error) State {
	var combined *jointError
	if errors.As(err, &combined) {
		return combined.observedState()
	}
	return standaloneState(err)
}

// IsCancellation reports the highest-precedence barrier outcome.
func IsCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func cancellationCause(causes ...error) error {
	for _, cause := range causes {
		if errors.Is(cause, context.Canceled) {
			return context.Canceled
		}
	}
	for _, cause := range causes {
		if errors.Is(cause, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
	}
	return nil
}
