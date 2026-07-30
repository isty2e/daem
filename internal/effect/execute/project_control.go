package execute

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func (authority *mutationAuthority) bindProjectStatefile(
	selectedRoot string,
	statefilePath string,
) error {
	if authority != nil && authority.projectStatefile != nil {
		return fmt.Errorf("project statefile authority is already bound")
	}
	destination, err := authority.bindProjectControlEntry(selectedRoot, statefilePath)
	if err != nil {
		return fmt.Errorf("bind project statefile: %w", err)
	}
	authority.projectStatefile = destination
	return nil
}

func (authority *mutationAuthority) bindRecoveryJournal(
	selectedRoot string,
	operationDir string,
) error {
	if authority != nil && authority.recoveryJournal != nil {
		return fmt.Errorf("recovery journal authority is already bound")
	}
	destination, err := authority.bindProjectControlEntry(selectedRoot, operationDir)
	if err != nil {
		return fmt.Errorf("bind recovery journal: %w", err)
	}
	authority.recoveryJournal = destination
	return nil
}

func (authority *mutationAuthority) bindProjectControlEntry(
	selectedRoot string,
	path string,
) (*rootedpath.EntryAuthority, error) {
	if authority == nil {
		return nil, fmt.Errorf("project mutation authority is unavailable")
	}
	return rootedpath.BindSelectedEntryAuthority(
		authority.capturedRoot,
		selectedRoot,
		path,
	)
}

func (authority *mutationAuthority) validateProjectSelection(selectedRoot string) error {
	if authority == nil || authority.capturedRoot == nil {
		return fmt.Errorf("project mutation authority is unavailable")
	}
	return authority.capturedRoot.ValidateSelection(selectedRoot)
}

func (authority *mutationAuthority) commitProjectStatefile(
	ctx context.Context,
	content []byte,
	mode os.FileMode,
) statefileCommitOutcome {
	if authority == nil || authority.filesystem == nil ||
		authority.projectStatefile == nil {
		return statefileCommitOutcome{
			status: statefileUncommitted,
			err:    fmt.Errorf("project statefile authority is unavailable"),
		}
	}
	err := commitRootedControlFile(
		ctx,
		authority.filesystem,
		authority.projectStatefile,
		content,
		mode,
	)
	if err == nil {
		return statefileCommitOutcome{status: statefileCommitted}
	}
	status := statefileUncommitted
	if mutationfs.MayHaveVisibleEffect(err) {
		status = statefileCommitIndeterminate
	}
	return statefileCommitOutcome{status: status, err: err}
}

func commitRootedControlFile(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	content []byte,
	mode os.FileMode,
) error {
	if filesystem == nil || authority == nil {
		return fmt.Errorf("rooted control-file authority is required")
	}
	capability, err := authority.Acquire()
	if err != nil {
		return err
	}
	expected, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if errors.Is(err, os.ErrNotExist) {
		return filesystem.CreateRootedFile(ctx, capability, content, mode)
	}
	if err != nil {
		_ = capability.Close()
		return err
	}
	return filesystem.ReplaceRootedFile(ctx, capability, content, mode, expected)
}

func (authority *mutationAuthority) retireActiveJournal(
	ctx context.Context,
	paths Paths,
	plan recovery.Plan,
) (returnErr error) {
	if authority == nil || authority.filesystem == nil {
		return fmt.Errorf("recovery journal retirement authority is unavailable")
	}
	if err := authority.validateProjectSelection(paths.ManifestRoot); err != nil {
		return err
	}
	root, err := rootedpath.CaptureRoot(paths.RecoveryDir)
	if err != nil {
		return fmt.Errorf("capture recovery root for journal retirement: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, root.Close())
	}()
	return journal.RetireActiveJournal(ctx, plan, root, authority.filesystem)
}
