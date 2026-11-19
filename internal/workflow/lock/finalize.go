package lock

import (
	"context"
	"errors"
	"fmt"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/effect/fileset"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	lockgenerate "github.com/isty2e/daem/internal/workflow/lock/generate"
	"github.com/isty2e/daem/internal/workflow/workspacecleanup"
)

// MetadataRecoveryStatus is the portion of a lock failure relevant to the
// selected declaration pair. A published fence does not prove eligibility.
type MetadataRecoveryStatus uint8

const (
	MetadataRecoveryNotApplicable MetadataRecoveryStatus = iota
	MetadataRecoveryPending
	MetadataRecoveryDifferentPair
	MetadataRecoveryUnfinishedPair
	MetadataRecoveryRetired
	MetadataRecoveryRetirementUnconfirmed
)

type afterMetadataRetirement struct {
	cause error
	steps []workspacecleanup.Step
}

func (err afterMetadataRetirement) Error() string { return err.cause.Error() }
func (err afterMetadataRetirement) Unwrap() error { return err.cause }

func MetadataRecoveryStatusOf(err error) MetadataRecoveryStatus {
	var retired afterMetadataRetirement
	switch {
	case errors.As(err, &retired) && retired.hasRetired(workspacecleanup.DeclarationPair):
		return MetadataRecoveryRetired
	case errors.Is(err, fileset.ErrFileSetRetirementUnconfirmed):
		return MetadataRecoveryRetirementUnconfirmed
	case !localPublishedFileSet(err):
		return MetadataRecoveryNotApplicable
	case errors.Is(err, fileset.ErrDifferentFinalizationPair):
		return MetadataRecoveryDifferentPair
	case errors.Is(err, fileset.ErrUnfinishedFinalizationPair):
		return MetadataRecoveryUnfinishedPair
	default:
		return MetadataRecoveryPending
	}
}

func (err afterMetadataRetirement) hasRetired(kind workspacecleanup.Kind) bool {
	for _, step := range err.steps {
		if step.Kind == kind && step.Outcome == workspacecleanup.Retired {
			return true
		}
	}
	return false
}

// JournalRetired reports whether lock's cleanup retired the journal before a
// later failure. The selected declaration record may still be pending.
func JournalRetired(err error) bool {
	var progress afterMetadataRetirement
	return errors.As(err, &progress) && progress.hasRetired(workspacecleanup.JournalRetirement)
}

func localPublishedFileSet(err error) bool {
	return !recoverygate.IsCancellation(err) && !recoverygate.IsGlobalMetadataRefusal(err) &&
		fileset.FileSetFenceKindOf(err) == fileset.FileSetFencePublishedTransaction
}

func finishLockMetadata(ctx context.Context, paths daempaths.Paths, stateDir recoverygate.StateDirAuthority) (*workspacecleanup.Plan, []workspacecleanup.Step, error) {
	barrier, err := recoverygate.NewEffectAuthorityForStateDir(ctx, paths, stateDir)
	if err != nil {
		return nil, nil, err
	}
	fenceErr := barrier.Validate(ctx)
	state := recoverygate.StateOf(fenceErr)
	if fenceErr == nil || (state.FileSetKnown() && state.FileSet() == fileset.FileSetFenceClear && !recoverygate.IsCancellation(fenceErr)) {
		// The declaration fence may already have been cleared. Do not turn a
		// journal-only observation into a lock cleanup request.
		return nil, nil, lockRequireStateDirClear(ctx, paths, stateDir)
	}
	if !localPublishedFileSet(fenceErr) {
		return nil, nil, fenceErr
	}
	pending, err := workspacecleanup.Observe(ctx, paths, barrier, fenceErr)
	if err != nil {
		return nil, nil, err
	}
	// Only the source declarations are needed for cleanup. The later lock
	// still owns placement validation and source resolution.
	content, err := pending.ReadSettledManifest(ctx)
	if err != nil {
		return nil, nil, err
	}
	environment, err := declarationmanifest.Decode(content)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid manifest: %w", err)
	}
	local, err := lockgenerate.ConsumedLocalPaths(lockgenerate.Input{Paths: paths, Environment: environment})
	if err != nil {
		return nil, nil, err
	}
	plan, err := pending.Seal(ctx, local)
	if err != nil {
		return nil, nil, err
	}
	steps, err := plan.Retire(ctx, nil, func(ctx context.Context) error { return ctx.Err() })
	return plan, steps, err
}
