//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

type preparedRootedTreeState uint8

const (
	preparedRootedTreeInvalid preparedRootedTreeState = iota
	preparedRootedTreeReady
	preparedRootedTreeTerminal
)

// PreparedRootedTree owns one private rooted stage and its destination-bound
// capability until Commit or Abort consumes both.
type PreparedRootedTree struct {
	mu                      sync.Mutex
	state                   preparedRootedTreeState
	destination             string
	anchor                  *anchoredParent
	stageName               string
	stagePath               string
	stageFD                 int
	stageObject             EntryIdentity
	expected                EntryIdentity
	limits                  mutationfs.TreeTraversalLimits
	rootMode                fs.FileMode
	rootModeSet             bool
	plannedEntries          map[string]preparedTreeEntryExpectation
	snapshot                preparedTreeSnapshot
	rootCreationMetadata    preparedTreeCreationMetadata
	modesMayRestrictCleanup bool
}

// PrepareRootedTree creates and populates a private stage beside the bound
// rooted destination. It consumes the capability on every outcome; on
// success the returned stage retains it until Commit or Abort. The writer is
// invalid after populate returns.
func PrepareRootedTree(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	populate func(mutationfs.RootedTreeWriter) error,
) (*PreparedRootedTree, error) {
	return PrepareRootedTreeWithLimits(
		ctx,
		capability,
		defaultTreeTraversalLimits(),
		populate,
	)
}

// PrepareRootedTreeWithLimits creates a private rooted stage whose writer and
// failure cleanup share one caller-owned finite traversal bound.
func PrepareRootedTreeWithLimits(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
	populate func(mutationfs.RootedTreeWriter) error,
) (*PreparedRootedTree, error) {
	if ctx == nil {
		if capability != nil {
			_ = capability.Close()
		}
		return nil, fmt.Errorf("rooted tree context is required")
	}
	if err := ctx.Err(); err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return nil, err
	}
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return nil, err
	}
	if populate == nil {
		_ = capability.Close()
		return nil, fmt.Errorf("rooted tree populate callback is required")
	}
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		_ = capability.Close()
		return nil, fmt.Errorf("rooted tree write limits: %w", err)
	}

	anchor, err := openRootedAnchoredParent(path, capability, true)
	if err != nil {
		failure := failFileBeforeVisibility(path, phaseCreateAncestors, err, anchor, "", EntryIdentity{}, faultPlan{})
		if anchor != nil {
			anchor.close()
		}
		_ = capability.Close()
		return nil, failure
	}
	prepared, err := createPreparedRootedTree(path, anchor, limits)
	if err != nil {
		var failure error
		if prepared != nil {
			prepared.mu.Lock()
			failure = prepared.failBeforeVisibilityLocked(phaseCreateTemporary, err, faultPlan{})
			prepared.mu.Unlock()
		} else {
			failure = failFileBeforeVisibility(path, phaseCreateTemporary, err, anchor, "", EntryIdentity{}, faultPlan{})
			anchor.close()
			_ = capability.Close()
		}
		return nil, failure
	}

	writer := &rootedTreeWriterUnix{ctx: ctx, prepared: prepared, budget: budget, active: true}
	returned := false
	defer func() {
		writer.deactivate()
		if !returned {
			_ = prepared.Abort(context.Background())
		}
	}()
	if err := populate(writer); err != nil {
		prepared.mu.Lock()
		failure := prepared.failBeforeVisibilityLocked(phaseWritePayload, err, faultPlan{})
		prepared.mu.Unlock()
		return nil, failure
	}
	writer.deactivate()
	if err := ctx.Err(); err != nil {
		prepared.mu.Lock()
		failure := prepared.failBeforeVisibilityLocked(phaseWritePayload, err, faultPlan{})
		prepared.mu.Unlock()
		return nil, failure
	}

	prepared.mu.Lock()
	err = prepared.captureSnapshotLocked(ctx)
	if err != nil {
		err = prepared.failBeforeVisibilityLocked(phaseValidate, err, faultPlan{})
	}
	prepared.mu.Unlock()
	if err != nil {
		return nil, err
	}
	returned = true
	return prepared, nil
}

// Commit publishes the prepared tree exactly once and consumes its capability.
func (prepared *PreparedRootedTree) Commit(ctx context.Context) error {
	_, err := prepared.CommitWithOutcome(ctx)
	return err
}

// CommitWithOutcome publishes the prepared tree and reports the strongest
// stable namespace conclusion plus any retained private stage name.
func (prepared *PreparedRootedTree) CommitWithOutcome(
	ctx context.Context,
) (mutationfs.CommitOutcome, error) {
	err := commitPreparedRootedTreeWithFaults(ctx, prepared, faultPlan{})
	return outcomeFromError(err), err
}

func commitPreparedRootedTreeWithFaults(
	ctx context.Context,
	prepared *PreparedRootedTree,
	faults faultPlan,
) error {
	if prepared == nil {
		return fmt.Errorf("prepared rooted tree is required")
	}
	if ctx == nil {
		return fmt.Errorf("rooted tree context is required")
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.state != preparedRootedTreeReady {
		return fmt.Errorf("prepared rooted tree is already consumed")
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseValidate, err, faults)
	}
	if err := prepared.verifyExpectedLocked(); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseValidate, err, faults)
	}
	if err := verifyPreparedTreeSnapshotLocked(ctx, prepared); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseValidate, err, faults)
	}
	if err := prepared.requireDestinationAbsentLocked(); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseValidate, err, faults)
	}
	if err := syncPreparedTreeSnapshotLocked(ctx, prepared, faults); err != nil {
		return prepared.failBeforeVisibilityLocked(errorPhase(err, phaseSyncTreeDirectory), err, faults)
	}
	if err := faults.check(ctx, phaseApplyMode); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseApplyMode, err, faults)
	}
	if err := verifyPreparedTreeSnapshotLocked(ctx, prepared); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseApplyMode, err, faults)
	}
	if err := faults.check(ctx, phaseRevalidateEntry); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseRevalidateEntry, err, faults)
	}
	if err := verifyPreparedTreeSnapshotLocked(ctx, prepared); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseRevalidateEntry, err, faults)
	}
	if err := faults.check(ctx, phaseCommitEntry); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseCommitEntry, err, faults)
	}
	if err := prepared.verifyExpectedLocked(); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseCommitEntry, err, faults)
	}
	if err := verifyPreparedTreeSnapshotLocked(ctx, prepared); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseCommitEntry, err, faults)
	}
	if err := prepared.requireDestinationAbsentLocked(); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseCommitEntry, err, faults)
	}
	if err := prepared.applyTreeModesLocked(ctx); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseApplyMode, err, faults)
	}
	if err := prepared.verifyExpectedLocked(); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseRevalidateEntry, err, faults)
	}
	if err := prepared.requireDestinationAbsentLocked(); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseRevalidateEntry, err, faults)
	}
	if err := ctx.Err(); err != nil {
		return prepared.failBeforeVisibilityLocked(phaseCommitEntry, err, faults)
	}

	err := renameNoReplace(
		prepared.anchor.parentFD(),
		prepared.stageName,
		prepared.anchor.parentFD(),
		prepared.anchor.base,
	)
	if err != nil {
		if errors.Is(err, fs.ErrExist) || errors.Is(err, unix.EEXIST) {
			if destinationErr := prepared.requireDestinationAbsentLocked(); destinationErr != nil {
				err = destinationErr
			}
		}
		return prepared.failBeforeVisibilityLocked(phaseCommitEntry, err, faults)
	}

	err = faults.run(ctx, phaseVerifyEntry, func() error {
		if err := prepared.anchor.verifyChain(); err != nil {
			return err
		}
		observed, _, err := prepared.anchor.observe(prepared.anchor.base, prepared.destination)
		if err != nil {
			return err
		}
		if !prepared.expected.sameObject(observed) {
			return fmt.Errorf("published rooted tree identity does not match prepared stage")
		}
		return nil
	})
	if err != nil {
		return prepared.finishVisibleLocked(phaseVerifyEntry, err)
	}
	if err := faults.run(ctx, phaseSyncParent, func() error {
		return syncDirectory(prepared.anchor.parentFD())
	}); err != nil {
		return prepared.finishVisibleLocked(phaseSyncParent, err)
	}
	if hasCreatedAncestors(prepared.anchor) {
		if err := faults.run(ctx, phaseSyncAncestors, func() error {
			return syncCreatedAncestors(prepared.anchor)
		}); err != nil {
			return prepared.finishVisibleLocked(phaseSyncAncestors, err)
		}
	}
	if err := prepared.anchor.verifyChain(); err != nil {
		return prepared.finishVisibleLocked(phaseVerifyEntry, err)
	}
	prepared.releaseLocked()
	return nil
}

// Abort removes an unpublished stage and consumes its capability. It is
// idempotent after either Commit or Abort reaches a terminal state.
func (prepared *PreparedRootedTree) Abort(ctx context.Context) error {
	if prepared == nil {
		return nil
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.state == preparedRootedTreeTerminal {
		return nil
	}
	if prepared.state != preparedRootedTreeReady {
		return fmt.Errorf("prepared rooted tree is not initialized")
	}
	residue := prepared.cleanupStageLocked(ctx, faultPlan{})
	prepared.releaseLocked()
	if len(residue) != 0 {
		return newFailure(
			failureRetainedResidue,
			phaseCleanupTemporary,
			prepared.destination,
			fmt.Errorf("could not safely remove unpublished rooted tree stage"),
			residue...,
		)
	}
	return nil
}

func (prepared *PreparedRootedTree) verifyStageObjectLocked() error {
	if err := prepared.anchor.verifyChain(); err != nil {
		return err
	}
	observed, _, err := prepared.anchor.observe(prepared.stageName, prepared.stagePath)
	if err != nil {
		return err
	}
	if !prepared.stageObject.sameObject(observed) {
		return fmt.Errorf("rooted tree stage binding changed")
	}
	opened, err := refreshOpenedIdentity(prepared.stageFD, prepared.stagePath)
	if err != nil {
		return err
	}
	if !prepared.stageObject.sameObject(opened) {
		return fmt.Errorf("rooted tree stage handle changed")
	}
	return prepared.anchor.capability.ValidateDirectoryHandle(uintptr(prepared.stageFD))
}

func (prepared *PreparedRootedTree) failBeforeVisibilityLocked(
	failedPhase phase,
	cause error,
	faults faultPlan,
) error {
	if err := prepared.anchor.verifyChain(); err != nil {
		prepared.releaseLocked()
		return newFailure(
			failureIndeterminateCommit,
			failedPhase,
			prepared.destination,
			errors.Join(cause, err),
		)
	}
	residue := prepared.cleanupStageLocked(context.Background(), faults)
	prepared.releaseLocked()
	kind := failureUncommitted
	if isUnsupported(cause) {
		kind = failureUnsupportedGuarantee
	}
	return newFailure(kind, failedPhase, prepared.destination, cause, residue...)
}

func (prepared *PreparedRootedTree) finishVisibleLocked(failedPhase phase, cause error) error {
	prepared.releaseLocked()
	return newFailure(failureIndeterminateCommit, failedPhase, prepared.destination, cause)
}

func (prepared *PreparedRootedTree) cleanupStageLocked(ctx context.Context, faults faultPlan) []string {
	var residue []string
	cleanupContext := context.Background()
	if ctx != nil {
		cleanupContext = context.WithoutCancel(ctx)
	}
	if faults.failures[phaseCleanupTemporary] != nil {
		residue = append(residue, prepared.stagePath)
	} else {
		observed, _, err := prepared.anchor.observe(prepared.stageName, prepared.stagePath)
		switch {
		case errors.Is(err, unix.ENOENT):
		case err != nil || !prepared.stageObject.sameObject(observed):
			residue = append(residue, prepared.stagePath)
		case prepared.normalizeStageModesForCleanupLocked(cleanupContext) != nil:
			residue = append(residue, prepared.stagePath)
		default:
			observed, _, err = prepared.anchor.observe(prepared.stageName, prepared.stagePath)
			if err != nil || !prepared.stageObject.sameObject(observed) {
				residue = append(residue, prepared.stagePath)
			} else if removeEntryAtWithFaults(
				cleanupContext,
				prepared.anchor.parentFD(),
				prepared.stageName,
				prepared.stagePath,
				observed,
				prepared.limits,
				faultPlan{},
				prepared.anchor.verifyChain,
			) != nil {
				residue = append(residue, prepared.stagePath)
			} else if syncDirectory(prepared.anchor.parentFD()) != nil {
				residue = append(residue, prepared.stagePath)
			}
		}
	}
	residue = append(residue, cleanupCreatedAncestors(prepared.anchor, faults)...)
	return residue
}

func (prepared *PreparedRootedTree) releaseLocked() {
	if prepared.stageFD >= 0 {
		_ = unix.Close(prepared.stageFD)
		prepared.stageFD = -1
	}
	if prepared.anchor != nil {
		capability := prepared.anchor.capability
		prepared.anchor.close()
		if capability != nil {
			_ = capability.Close()
		}
		prepared.anchor = nil
	}
	prepared.state = preparedRootedTreeTerminal
}
