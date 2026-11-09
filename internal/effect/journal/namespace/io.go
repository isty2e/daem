package namespace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/removalnamespace"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// ObserveRemovalNamespace checks fresh parent facts using the retained writer
// route. It neither reobserves aliases nor authorizes a slot mutation.
func (namespace DocumentNamespace) ObserveRemovalNamespace(ctx context.Context, authority recovery.RemovalNamespaceAuthority, budget *physicalwork.Budget) (recovery.RemovalNamespaceObservation, error) {
	if err := namespace.observation.Validate(); err != nil {
		return recovery.RemovalNamespaceObservation{}, err
	}
	return removalnamespace.Observe(ctx, namespace.writerRoot, namespace.writer, authority, budget)
}

func (grant RootGrant) ObserveRoute(ctx context.Context, destination output.Destination, budget *physicalwork.Budget) (mutation.DirectoryEntryAuthorityObservation, error) {
	selected, err := rootedpath.NewRelativeDestination(destination.RelativePath())
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	capability, err := grant.acquireRoute(ctx, selected, budget)
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	return ObserveWriterCapability(ctx, capability, budget)
}

func (alias Alias) ObserveRoute(ctx context.Context, budget *physicalwork.Budget) (mutation.DirectoryEntryAuthorityObservation, error) {
	if alias.root.selection.endpoint != nil {
		return alias.root.observeEndpointRoute(ctx, budget)
	}
	capability, err := alias.root.acquireRoute(ctx, alias.relative, budget)
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	return ObserveWriterCapability(ctx, capability, budget)
}

func (grant RootGrant) observeEndpointRoute(ctx context.Context, budget *physicalwork.Budget) (mutation.DirectoryEntryAuthorityObservation, error) {
	if ctx == nil || budget == nil {
		return mutation.DirectoryEntryAuthorityObservation{}, fmt.Errorf("endpoint route observation requires context and original allocation")
	}
	if err := grant.selection.qualifyLocator(ctx, budget); err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	if grant.selection.creation != nil {
		if _, err := grant.selection.qualifyCreation(ctx, budget); err != nil {
			return mutation.DirectoryEntryAuthorityObservation{}, err
		}
	} else if err := grant.selection.capturedRoot().ValidateSelectionBounded(grant.selection.SelectedPath, physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	route, _, err := endpointRoutePath(ctx, grant.selection.endpoint.selected, budget)
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(route, physicalwork.MaximumPhysicalPathDepth, budget)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	return observed, nil
}

func (grant RootGrant) acquireRoute(ctx context.Context, selected rootedpath.RelativeDestination, budget *physicalwork.Budget) (rootedpath.CommitCapability, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if grant.selection.endpoint != nil {
		return nil, fmt.Errorf("endpoint witness cannot authorize a directory-relative route")
	}
	if grant.selection.Root == nil {
		return nil, fmt.Errorf("writer route requires its selected retained root")
	}
	if err := selected.Validate(); err != nil {
		return nil, err
	}

	physical := grant.authority.PhysicalRoot()
	if grant.selection.creation != nil {
		var err error
		physical, err = grant.selection.qualifyCreation(ctx, budget)
		if err != nil {
			return nil, err
		}
	} else if err := grant.selection.Root.ValidateSelectionBounded(grant.selection.SelectedPath, physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
		return nil, err
	}
	path, err := filepath.Rel(grant.authority.PhysicalRoot(), filepath.Join(physical, filepath.FromSlash(selected.Path())))
	if err != nil {
		return nil, err
	}
	relative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(path))
	if err != nil {
		return nil, err
	}
	bound, err := grant.authority.Bind(relative)
	if err != nil {
		return nil, err
	}
	return grant.selection.Root.AcquireBounded(bound, physicalwork.MaximumPhysicalPathDepth, budget)
}

func (namespace DocumentNamespace) ReadReferent(ctx context.Context, maximumBytes int64, budget *physicalwork.Budget) (aggregate.Document, *recovery.PermissionMode, mutationfs.EntryIdentity, error) {
	if maximumBytes <= 0 || maximumBytes > recovery.MaximumRecoveryBackupFileBytes || budget == nil {
		return aggregate.Document{}, nil, nil, fmt.Errorf("native document read limit or shared budget is invalid")
	}
	limit := min(maximumBytes, budget.RemainingBytes())
	capability, err := namespace.AcquireReferent(ctx, budget)
	if err != nil {
		return aggregate.Document{}, nil, nil, err
	}
	content, mode, identity, readErr := namespace.filesystem.ReadRootedRegularFileUpTo(ctx, capability, limit)
	closeErr := capability.Close()
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		charged = limit
	}
	work, workErr := workmeasure.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if closeErr == nil && workErr == nil && errors.Is(readErr, fs.ErrNotExist) {
		return aggregate.AbsentDocument(), nil, nil, ctx.Err()
	}
	if err := errors.Join(readErr, closeErr, workErr, ctx.Err()); err != nil {
		return aggregate.Document{}, nil, nil, err
	}
	return aggregate.ExistingDocument(content), recovery.NewPermissionMode(mode), identity, nil
}
