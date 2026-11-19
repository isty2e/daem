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
	return checkRebuildInputs(ctx, path, input.Paths.ManifestPath, localPaths, nil)
}

func checkRebuildInputs(ctx context.Context, path, manifestPath string, localPaths []string, revisions *mutation.RevisionSet) error {
	depth := physicalwork.MaximumPhysicalPathDepth
	budget := physicalwork.NewPathBudget()
	observe := func(request mutation.RevisionRequest) (mutation.DirectoryEntryAuthorityObservation, bool, error) {
		if err := ctx.Err(); err != nil {
			return mutation.DirectoryEntryAuthorityObservation{}, false, err
		}
		if revisions == nil {
			_, authority, err := mutation.ObserveFutureReferentAuthorityBounded(request.Path, depth, budget)
			_, exact := authority.Exact()
			return authority, exact, err
		}
		physical, present, err := revisions.CapturedReferent(request)
		if err != nil || !present {
			return mutation.DirectoryEntryAuthorityObservation{}, false, err
		}
		authority, err := mutation.ObserveDirectoryEntryAuthorityBounded(physical, depth, budget)
		return authority, true, err
	}
	lockRequest, err := mutation.NewBoundedFileRevisionRequest(declarationartifact.MaximumBytes, path, mutation.PathEffectReferent)
	if err != nil {
		return err
	}
	destination, comparable, err := observe(lockRequest)
	if err != nil {
		return err
	}
	if !comparable {
		return mutation.StaleSnapshotError{}
	}

	check := func(request mutation.RevisionRequest) error {
		authority, comparable, err := observe(request)
		if err != nil {
			return err
		}
		// Missing inputs cannot overlap a present lock. Let source resolution
		// report the absence, not a collision with a hypothetical future name.
		if comparable && destination.MayOverlap(authority) {
			return fmt.Errorf("cannot rebuild damaged lockfile: its destination overlaps the selected manifest or a local source")
		}
		return nil
	}
	manifestRequest, err := mutation.NewBoundedFileRevisionRequest(declarationartifact.MaximumBytes, manifestPath, mutation.PathEffectReferent)
	if err != nil {
		return err
	}
	if err := check(manifestRequest); err != nil {
		return err
	}
	for _, source := range localPaths {
		if err := check(mutation.NewBoundedContentRevisionRequest(source, mutation.PathEffectReferent)); err != nil {
			return err
		}
	}
	return nil
}
