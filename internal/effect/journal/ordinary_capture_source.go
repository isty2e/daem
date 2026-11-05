package journal

import (
	"context"
	"errors"
	"fmt"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func validateOrdinaryCaptureBackups(ctx context.Context, directory string, view access.View, entries []journalrecord.PathEntry, filesystem mutationfs.RootedReader, budget *physicalwork.Budget) (resultErr error) {
	references, err := journalrecord.BackupReferences(ctx, entries)
	if err != nil {
		return err
	}
	unit, err := workmeasure.NewArtifactWork(1, 0)
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
		if !present || string(entry.Kind()) != before.Kind() {
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

	root, err := rootedpath.CaptureRootBounded(directory, physicalwork.MaximumPhysicalPathDepth, budget)
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
		relative, err := rootedpath.NewRelativeDestination(before.Path())
		if err != nil {
			return err
		}
		destination, err := authority.Bind(relative)
		if err != nil {
			return err
		}
		capability, err := root.AcquireBounded(destination, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		observed, observeErr := journalqualification.ObserveBackup(ctx, before.Path(), filesystem, capability, budget)
		if err := errors.Join(observeErr, capability.Close()); err != nil {
			return err
		}
		if !observed.Exists || observed.Error != "" || !before.Matches(observed.Kind, observed.ContentHash) {
			return fmt.Errorf("ordinary backup source does not match the captured before-state")
		}
	}
	return ctx.Err()
}
