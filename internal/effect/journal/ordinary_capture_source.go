package journal

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func validateOrdinaryCaptureBackups(ctx context.Context, directory string, view access.View, entries []recoveryEntry, filesystem mutationfs.RootedReader, budget *recovery.PhysicalWorkBudget) (resultErr error) {
	references := make(map[string]recoveryBeforePathDTO)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Before.BackupPath == "" {
			continue
		}
		name, present := strings.CutPrefix(entry.Before.BackupPath, "files/")
		if !present || name == "." || name == ".." || name == "" || path.Base(name) != name {
			return fmt.Errorf("ordinary builder returned a noncanonical backup reference")
		}
		if _, duplicate := references[name]; duplicate {
			return fmt.Errorf("ordinary builder returned duplicate backup references")
		}
		references[name] = entry.Before
	}
	unit, err := recovery.NewArtifactWork(1, 0)
	if err != nil {
		return err
	}
	if budget.RemainingEntries() == 0 {
		return fmt.Errorf("ordinary source inventory requires an entry allowance")
	}
	rootEntries := 0
	if err := view.VisitDirectory(ctx, ".", func(entry access.Entry) error {
		if err := budget.AdmitTree(unit); err != nil {
			return err
		}
		rootEntries++
		if len(references) == 0 || entry.Name() != "files" || entry.Kind() != access.EntryKindDirectory {
			return fmt.Errorf("ordinary backup source contains an unreferenced root entry")
		}
		return nil
	}); err != nil {
		return err
	}
	if len(references) == 0 {
		return nil
	}
	if rootEntries != 1 {
		return fmt.Errorf("ordinary backup source is missing its files directory")
	}
	if budget.RemainingEntries() <= len(references) {
		return fmt.Errorf("ordinary backup inventory exceeds its remaining entry and overflow allowance")
	}
	seen := 0
	if err := view.VisitDirectory(ctx, "files", func(entry access.Entry) error {
		if err := budget.AdmitTree(unit); err != nil {
			return err
		}
		before, present := references[entry.Name()]
		if !present || string(entry.Kind()) != before.Kind {
			return fmt.Errorf("ordinary backup source contains an unreferenced or wrong-kind backup")
		}
		seen++
		return nil
	}); err != nil {
		return err
	}
	if seen != len(references) {
		return fmt.Errorf("ordinary backup source is missing referenced backups")
	}

	root, err := rootedpath.CaptureRootBounded(directory, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		return err
	}
	for _, before := range references {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := rootedpath.NewRelativeDestination(before.BackupPath)
		if err != nil {
			return err
		}
		destination, err := authority.Bind(relative)
		if err != nil {
			return err
		}
		capability, err := root.AcquireBounded(destination, recovery.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		observed, observeErr := observeRecoveryBackup(ctx, before.BackupPath, filesystem, capability, budget)
		if err := errors.Join(observeErr, capability.Close()); err != nil {
			return err
		}
		if !observed.Exists || observed.Error != "" || observed.Kind != before.Kind || observed.ContentHash != before.ContentHash {
			return fmt.Errorf("ordinary backup source does not match the captured before-state")
		}
	}
	return ctx.Err()
}
