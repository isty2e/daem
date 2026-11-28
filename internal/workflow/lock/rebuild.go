package lock

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	lockgenerate "github.com/isty2e/daem/internal/workflow/lock/generate"
)

func checkPreviewRebuildInputs(ctx context.Context, path string, input lockgenerate.Input) error {
	localPaths, err := lockgenerate.ConsumedLocalPaths(input)
	if err != nil {
		return err
	}
	depth := physicalwork.MaximumPhysicalPathDepth
	budget := physicalwork.NewPathBudget()
	observe := func(path string) (mutation.DirectoryEntryAuthorityObservation, bool, error) {
		if err := ctx.Err(); err != nil {
			return mutation.DirectoryEntryAuthorityObservation{}, false, err
		}
		_, authority, err := mutation.ObserveFutureReferentAuthorityBounded(path, depth, budget)
		_, exact := authority.Exact()
		return authority, exact, err
	}
	destination, comparable, err := observe(path)
	if err != nil {
		return err
	}
	if !comparable {
		return mutation.StaleSnapshotError{}
	}

	check := func(path string) error {
		authority, comparable, err := observe(path)
		if err != nil {
			return err
		}
		// Missing inputs cannot overlap a present lock. Let source resolution
		// report the absence, not a collision with a hypothetical future name.
		if comparable && destination.MayOverlap(authority) {
			return fmt.Errorf("cannot rebuild damaged lockfile: its %w", declarationartifact.ErrLockfileInputOverlap)
		}
		return nil
	}
	if err := check(input.Paths.ManifestPath); err != nil {
		return err
	}
	for _, source := range localPaths {
		if err := check(source); err != nil {
			return err
		}
	}
	return nil
}
