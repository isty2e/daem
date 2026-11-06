package hostmutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/removalnamespace"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

const maximumRollbackScratchTreeDepth = recovery.MaximumArtifactTreeDepth + 1

type ScratchArtifact struct {
	Path string
	Work workmeasure.ArtifactWork
}

type RollbackScratch struct {
	*rollbackScratchState
}

type rollbackScratchState struct {
	directory    string
	filesystem   mutationfs.RootedStore
	cleanupState *preparedRollbackCleanup
}

type preparedRollbackCleanup struct {
	root      *rootedpath.CapturedRoot
	authority *rootedpath.EntryAuthority
	identity  mutationfs.EntryIdentity
	limits    mutationfs.TreeTraversalLimits
	complete  bool
}

type recoveryScratchPathReservation struct{ budget *physicalwork.Budget }

func (reservation recoveryScratchPathReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveScratchPathComponents(count)
}

func CreateRollbackScratch(filesystem mutationfs.RootedStore) (*RollbackScratch, error) {
	directory, err := os.MkdirTemp("", "daem-recovery-rollback-")
	if err != nil {
		return nil, fmt.Errorf("create recovery rollback directory: %w", err)
	}
	return &RollbackScratch{rollbackScratchState: &rollbackScratchState{
		directory:  directory,
		filesystem: filesystem,
	}}, nil
}

func (scratch *RollbackScratch) Directory() string {
	if scratch == nil || scratch.rollbackScratchState == nil {
		return ""
	}
	return scratch.directory
}

func (scratch *RollbackScratch) PrepareCleanup(
	ctx context.Context,
	budget *physicalwork.Budget,
	artifacts []ScratchArtifact,
) (returnErr error) {
	if scratch == nil || scratch.rollbackScratchState == nil || scratch.directory == "" {
		return fmt.Errorf("recovery rollback stage is unavailable")
	}
	if scratch.cleanupState != nil {
		return fmt.Errorf("recovery rollback cleanup is already prepared")
	}
	if budget == nil || scratch.filesystem == nil {
		return fmt.Errorf("recovery rollback cleanup requires bounded mutation authority")
	}

	work, err := scratch.cleanupWork(artifacts)
	if err != nil {
		return err
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(scratch.directory, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return fmt.Errorf("capture recovery rollback cleanup authority: %w", err)
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, root.Close())
		}
	}()

	reservation := recoveryScratchPathReservation{budget: budget}
	for range 3 {
		if err := root.ReserveDestinationAccess(destination, physicalwork.MaximumPhysicalPathDepth, reservation); err != nil {
			return fmt.Errorf("reserve recovery rollback cleanup path: %w", err)
		}
	}
	if err := removalnamespace.ReserveScratchRootedCleanupWork(budget, destination, work); err != nil {
		return fmt.Errorf("reserve recovery rollback cleanup tree: %w", err)
	}
	executionBudget, reservedWork, err := budget.BeginReservedScratchCleanup()
	if err != nil {
		return err
	}
	entryAuthority, err := rootedpath.BindCapturedEntryAuthorityBounded(root, destination, physicalwork.MaximumPhysicalPathDepth, executionBudget)
	if err != nil {
		return fmt.Errorf("bind recovery rollback cleanup entry: %w", err)
	}
	capability, err := entryAuthority.Acquire()
	if err != nil {
		return errors.Join(err, entryAuthority.Close())
	}
	identity, err := scratch.filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return errors.Join(err, capability.Close(), entryAuthority.Close())
	}
	if identity.Kind() != mutationfs.EntryKindDirectory {
		return errors.Join(fmt.Errorf("recovery rollback stage is not a directory"), capability.Close(), entryAuthority.Close())
	}
	if err := capability.Close(); err != nil {
		return errors.Join(err, entryAuthority.Close())
	}
	limits, err := mutationfs.NewTreeTraversalLimits(reservedWork.Entries(), maximumRollbackScratchTreeDepth, reservedWork.Bytes())
	if err != nil {
		return errors.Join(err, entryAuthority.Close())
	}
	scratch.cleanupState = &preparedRollbackCleanup{
		root: root, authority: entryAuthority, identity: identity, limits: limits,
	}
	return nil
}

func (scratch *RollbackScratch) cleanupWork(artifacts []ScratchArtifact) (workmeasure.ArtifactWork, error) {
	seen := make(map[string]struct{}, len(artifacts))
	entries := 0
	var bytes int64
	for _, artifact := range artifacts {
		backupPath := filepath.Clean(artifact.Path)
		if backupPath == scratch.directory || !strings.HasPrefix(backupPath, scratch.directory+string(filepath.Separator)) {
			return workmeasure.ArtifactWork{}, fmt.Errorf("rollback backup escaped its private stage")
		}
		if _, duplicate := seen[backupPath]; duplicate {
			continue
		}
		seen[backupPath] = struct{}{}
		entries += artifact.Work.Entries() + 1
		bytes += artifact.Work.Bytes()
	}

	return workmeasure.NewArtifactWork(entries, bytes)
}

func (scratch *RollbackScratch) Cleanup(ctx context.Context) error {
	if scratch == nil || scratch.rollbackScratchState == nil || scratch.directory == "" {
		return nil
	}
	if scratch.cleanupState == nil {
		return fmt.Errorf("recovery rollback cleanup was not prepared")
	}
	if scratch.cleanupState.complete {
		return nil
	}
	if scratch.filesystem == nil {
		return fmt.Errorf("recovery rollback cleanup filesystem is unavailable")
	}

	return scratch.cleanupState.execute(ctx, scratch.filesystem)
}

func (cleanup *preparedRollbackCleanup) execute(ctx context.Context, filesystem mutationfs.RootedStore) (returnErr error) {
	if cleanup == nil || cleanup.root == nil || cleanup.authority == nil || cleanup.identity == nil {
		return fmt.Errorf("recovery rollback cleanup is uninitialized")
	}
	defer func() {
		returnErr = errors.Join(returnErr, cleanup.authority.Close(), cleanup.root.Close())
		if returnErr == nil {
			cleanup.complete = true
		}
	}()

	capability, err := cleanup.authority.Acquire()
	if err != nil {
		return err
	}
	current, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	if !cleanup.identity.Equal(current) {
		return errors.Join(fmt.Errorf("recovery rollback stage changed before cleanup"), capability.Close())
	}
	outcome, err := filesystem.CleanupRootedEntry(ctx, capability, current, cleanup.limits)
	if err != nil {
		return fmt.Errorf("cleanup recovery rollback stage (%s): %w", outcome.State(), err)
	}
	return nil
}

func (scratch *RollbackScratch) Abort(ctx context.Context, artifacts []ScratchArtifact) error {
	if scratch == nil || scratch.rollbackScratchState == nil || scratch.directory == "" {
		return nil
	}
	if scratch.cleanupState != nil {
		return scratch.Cleanup(ctx)
	}
	if scratch.filesystem == nil {
		return fmt.Errorf("recovery rollback abort cleanup filesystem is unavailable")
	}

	work, err := scratch.cleanupWork(artifacts)
	if err != nil {
		return err
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		return err
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(scratch.directory, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	reservation := recoveryScratchPathReservation{budget: budget}
	for range 3 {
		if err := root.ReserveDestinationAccess(destination, physicalwork.MaximumPhysicalPathDepth, reservation); err != nil {
			return errors.Join(err, root.Close())
		}
	}
	if err := removalnamespace.ReserveScratchRootedCleanupWork(budget, destination, work); err != nil {
		return errors.Join(err, root.Close())
	}
	executionBudget, reservedWork, err := budget.BeginReservedScratchCleanup()
	if err != nil {
		return errors.Join(err, root.Close())
	}
	entryAuthority, err := rootedpath.BindCapturedEntryAuthorityBounded(root, destination, physicalwork.MaximumPhysicalPathDepth, executionBudget)
	if err != nil {
		return errors.Join(err, root.Close())
	}
	capability, err := entryAuthority.Acquire()
	if err != nil {
		return errors.Join(err, entryAuthority.Close(), root.Close())
	}
	identity, err := scratch.filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return errors.Join(err, capability.Close(), entryAuthority.Close(), root.Close())
	}
	limits, err := mutationfs.NewTreeTraversalLimits(reservedWork.Entries(), maximumRollbackScratchTreeDepth, reservedWork.Bytes())
	if err != nil {
		return errors.Join(err, capability.Close(), entryAuthority.Close(), root.Close())
	}
	outcome, cleanupErr := scratch.filesystem.CleanupRootedEntry(ctx, capability, identity, limits)
	return errors.Join(func() error {
		if cleanupErr == nil {
			return nil
		}
		return fmt.Errorf("abort recovery rollback stage (%s): %w", outcome.State(), cleanupErr)
	}(), entryAuthority.Close(), root.Close())
}
