package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type hostRollback struct {
	dir     string
	entries []hostRollbackEntry
}

type hostRollbackEntry struct {
	destination      mutationDestination
	existed          bool
	kind             string
	maximumFileBytes int64
	backupPath       string
	backup           recoveryBackup
	fileMode         os.FileMode
	identity         mutationfs.EntryIdentity
	stagedState      recoveryWholePathState
	effectState      recoveryWholePathState
	effectKnown      bool
	attempted        bool
}

type rollbackStageAction struct {
	actionIndex int
	destination mutationDestination
	action      recoveryHostAction
}

type recoveryWholePathState struct {
	existed     bool
	kind        string
	contentHash string
	fileMode    os.FileMode
}

func (state recoveryWholePathState) equal(other recoveryWholePathState) bool {
	return state.existed == other.existed &&
		state.kind == other.kind &&
		state.contentHash == other.contentHash &&
		state.fileMode.Perm() == other.fileMode.Perm()
}

func stageRollbackDestinations(
	ctx context.Context,
	authority *mutationAuthority,
	actions []rollbackStageAction,
	rollbackDir string,
	index int,
	codecs aggregate.CodecCatalog,
) ([]hostRollbackEntry, error) {
	if len(actions) == 0 {
		return nil, fmt.Errorf("rollback stage requires at least one action")
	}
	physicalDestination := filepath.Clean(actions[0].destination.hostPath)
	for _, action := range actions {
		if !action.destination.isRooted() {
			return nil, fmt.Errorf("rollback destination is invalid")
		}
		if filepath.Clean(action.destination.hostPath) != physicalDestination {
			return nil, fmt.Errorf("rollback stage actions do not share one physical destination")
		}
	}
	return stageRootedRollbackDestinations(ctx, authority, actions, rollbackDir, index, codecs)
}

func stageRecoveryRollback(
	ctx context.Context,
	authority *mutationAuthority,
	actions []recoveryHostAction,
	codecs aggregate.CodecCatalog,
) (hostRollback, error) {
	rollbackDir, err := os.MkdirTemp("", "daem-recovery-rollback-")
	if err != nil {
		return hostRollback{}, fmt.Errorf("create recovery rollback directory: %w", err)
	}

	rollback := hostRollback{
		dir:     rollbackDir,
		entries: make([]hostRollbackEntry, len(actions)),
	}
	stageGroups := make([][]rollbackStageAction, 0, len(actions))
	stageGroupByDestination := make(map[string]int, len(actions))
	for index, action := range actions {
		logical, err := recoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return hostRollback{}, errors.Join(err, rollback.cleanup())
		}
		destination, err := authority.resolveBoundDestination(action.Scope, logical)
		if err != nil {
			return hostRollback{}, errors.Join(err, rollback.cleanup())
		}
		key := filepath.Clean(destination.hostPath)
		groupIndex, present := stageGroupByDestination[key]
		if !present {
			groupIndex = len(stageGroups)
			stageGroupByDestination[key] = groupIndex
			stageGroups = append(stageGroups, nil)
		}
		stageGroups[groupIndex] = append(stageGroups[groupIndex], rollbackStageAction{
			actionIndex: index,
			destination: destination,
			action:      action,
		})
	}
	for groupIndex, group := range stageGroups {
		entries, err := stageRollbackDestinations(ctx, authority, group, rollbackDir, groupIndex, codecs)
		if err != nil {
			return hostRollback{}, errors.Join(err, rollback.cleanup())
		}
		if len(entries) != len(group) {
			return hostRollback{}, errors.Join(
				fmt.Errorf("rollback stage entry count %d does not match action count %d", len(entries), len(group)),
				rollback.cleanup(),
			)
		}
		for index, entry := range entries {
			rollback.entries[group[index].actionIndex] = entry
		}
	}

	return rollback, nil
}

func (rollback hostRollback) restore(
	ctx context.Context,
	authority *mutationAuthority,
	gate visibilityEffectGate,
) error {
	var failures []error
	groups := make(map[string][]hostRollbackEntry, len(rollback.entries))
	for _, entry := range rollback.entries {
		key := filepath.Clean(entry.destination.hostPath)
		groups[key] = append(groups[key], entry)
	}
	restored := make(map[string]struct{})
	for index := len(rollback.entries) - 1; index >= 0; index-- {
		key := filepath.Clean(rollback.entries[index].destination.hostPath)
		if _, present := restored[key]; present {
			continue
		}
		restored[key] = struct{}{}
		if !rollbackGroupAttempted(groups[key]) {
			continue
		}
		if err := gate.validateBefore(ctx); err != nil {
			failures = append(failures, fmt.Errorf("validate recovery rollback authority: %w", err))
			continue
		}
		if err := restoreRollbackGroup(ctx, authority, groups[key]); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := gate.acceptAfter(ctx); err != nil {
			failures = append(failures, fmt.Errorf("accept recovery rollback visibility: %w", err))
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}

	return nil
}

func rollbackGroupAttempted(entries []hostRollbackEntry) bool {
	for _, entry := range entries {
		if entry.attempted {
			return true
		}
	}
	return false
}

func (rollback hostRollback) cleanup() error {
	if rollback.dir == "" {
		return nil
	}
	if err := makeRollbackTreeWritable(rollback.dir); err != nil {
		return err
	}
	return os.RemoveAll(rollback.dir)
}

func makeRollbackTreeWritable(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("rollback stage %q contains a symlink", path)
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o700)
		}
		return nil
	})
}

func restoreRollbackGroup(
	ctx context.Context,
	authority *mutationAuthority,
	entries []hostRollbackEntry,
) error {
	if len(entries) == 0 {
		return nil
	}
	baseline := entries[0]
	attempted := false
	for _, entry := range entries {
		if !entry.stagedState.equal(baseline.stagedState) {
			return fmt.Errorf("rollback stage for %q has inconsistent physical baselines", baseline.destination.logical)
		}
		if entry.maximumFileBytes != baseline.maximumFileBytes {
			return fmt.Errorf("rollback stage for %q has inconsistent file byte limits", baseline.destination.logical)
		}
		attempted = attempted || entry.attempted
	}
	if !attempted {
		return nil
	}
	current, identity, err := observeRecoveryWholePathState(
		ctx,
		authority,
		baseline.destination,
		baseline.maximumFileBytes,
	)
	if err != nil {
		return err
	}
	if current.equal(baseline.stagedState) {
		return nil
	}
	matchesEffect := false
	for _, entry := range entries {
		if entry.attempted && entry.effectKnown && current.equal(entry.effectState) {
			matchesEffect = true
			break
		}
	}
	if !matchesEffect {
		return fmt.Errorf("rollback destination %q changed outside the recovery attempt", baseline.destination.logical)
	}
	if !baseline.existed {
		if err := removeDestinationAgainst(ctx, authority, baseline.destination, identity); err != nil {
			return fmt.Errorf("rollback remove %q: %w", baseline.destination.logical, err)
		}
		return nil
	}
	switch baseline.kind {
	case recovery.PathKindFile:
		content, err := baseline.backup.readFile(ctx)
		if err != nil {
			return fmt.Errorf("read rollback file for %q: %w", baseline.destination.logical, err)
		}
		if err := commitFileDestinationAgainst(
			ctx,
			authority,
			baseline.destination,
			content,
			baseline.fileMode,
			current.existed,
			identity,
		); err != nil {
			return fmt.Errorf("rollback restore file %q: %w", baseline.destination.logical, err)
		}
	case recovery.PathKindDirectory:
		if err := commitRecoveryDirectoryDestinationAgainst(
			ctx,
			authority,
			baseline.backup,
			baseline.destination,
			current.existed,
			identity,
		); err != nil {
			return fmt.Errorf("rollback restore directory %q: %w", baseline.destination.logical, err)
		}
	default:
		return fmt.Errorf("rollback kind %q for %q is not supported", baseline.kind, baseline.destination.logical)
	}
	return nil
}

func rollbackError(primary error, rollback error) error {
	if rollback == nil {
		return primary
	}

	return fmt.Errorf("%w; rollback failed: %v", primary, rollback)
}
