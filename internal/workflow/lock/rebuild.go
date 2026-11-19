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
	observe := func(request mutation.RevisionRequest) (mutation.DirectoryEntryAuthorityObservation, error) {
		if err := ctx.Err(); err != nil {
			return mutation.DirectoryEntryAuthorityObservation{}, err
		}
		if revisions == nil {
			_, authority, err := mutation.ObserveFutureReferentAuthorityBounded(request.Path, depth, budget)
			return authority, err
		}
		physical, _, err := revisions.CapturedReferent(request)
		if err != nil {
			return mutation.DirectoryEntryAuthorityObservation{}, err
		}
		return mutation.ObserveDirectoryEntryAuthorityBounded(physical, depth, budget)
	}
	lockRequest, err := mutation.NewBoundedFileRevisionRequest(declarationartifact.MaximumBytes, path, mutation.PathEffectReferent)
	if err != nil {
		return err
	}
	destination, err := observe(lockRequest)
	if err != nil {
		return err
	}

	check := func(request mutation.RevisionRequest) error {
		authority, err := observe(request)
		if err != nil {
			return err
		}
		if destination.MayOverlap(authority) {
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
