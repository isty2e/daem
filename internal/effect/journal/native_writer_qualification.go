package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type ordinaryWriterAddress struct {
	scope       target.Scope
	destination output.Destination
}

func (record nativeJournalRecord) qualifyWriterPartition(ctx context.Context, namespaces []NativeDocumentNamespace, manifest nativeDocumentRootGrant, ordinaryCapabilities RootedCapabilityResolver, budget *recovery.PhysicalWorkBudget, protected []pathauthority.Exact) error {
	var paths []documentWriterPath
	appendObservation := func(observation mutation.DirectoryEntryAuthorityObservation, group int) error {
		if err := observation.Validate(); err != nil {
			return err
		}
		volume, components := recoveryFilesystemPathComponents(nativeDocumentObservationKey(observation))
		paths = append(paths, documentWriterPath{volume, components, group})
		return nil
	}
	for index, namespace := range namespaces {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := appendObservation(namespace.observation, index); err != nil {
			return err
		}
		for _, alias := range namespace.aliases {
			capability, err := acquireNativeWriterRoute(ctx, alias.root, alias.address.AggregateRoot(), budget)
			if err != nil {
				return err
			}
			observation, err := observeNativeWriterCapability(ctx, capability, budget)
			if err != nil {
				return err
			}
			if err := appendObservation(observation, index); err != nil {
				return err
			}
		}
	}
	seen := make(map[ordinaryWriterAddress]recoveryGlobalPathBinding)
	for _, entry := range record.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return err
		}
		address := ordinaryWriterAddress{target.Scope(entry.Scope), destination}
		binding := recoveryGlobalPathBinding{}
		if entry.GlobalPathBinding != nil {
			binding = *entry.GlobalPathBinding
		}
		if previous, duplicate := seen[address]; duplicate {
			if previous != binding {
				return fmt.Errorf("ordinary writer repeats a logical address with different root evidence")
			}
			continue
		}
		seen[address] = binding
		var capability rootedpath.CommitCapability
		switch target.Scope(entry.Scope) {
		case target.ScopeProject:
			if destination.RootRole() != output.RootProject {
				return fmt.Errorf("ordinary project writer has no project destination")
			}
			capability, err = acquireNativeWriterRoute(ctx, manifest, destination, budget)
		case target.ScopeGlobal:
			capability, err = acquireNativeOrdinaryGlobalWriter(entry, destination, ordinaryCapabilities, budget)
		default:
			err = fmt.Errorf("ordinary writer has an unsupported scope")
		}
		if err != nil {
			return err
		}
		observation, err := observeNativeWriterCapability(ctx, capability, budget)
		if err != nil {
			return err
		}
		if err := appendObservation(observation, -1); err != nil {
			return err
		}
	}
	for index, path := range protected {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := path.Validate(); err != nil {
			return err
		}
		volume, components := recoveryFilesystemPathComponents(path.Key())
		paths = append(paths, documentWriterPath{volume, components, -2 - index})
	}
	return validateDocumentWriterPaths(ctx, paths)
}

func acquireNativeWriterRoute(ctx context.Context, grant nativeDocumentRootGrant, destination output.Destination, budget *recovery.PhysicalWorkBudget) (rootedpath.CommitCapability, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if grant.selection.Root == nil {
		return nil, fmt.Errorf("writer route requires its selected retained root")
	}

	physical := grant.authority.PhysicalRoot()
	if grant.selection.creation != nil {
		var err error
		physical, err = grant.selection.qualifyCreation(ctx, budget)
		if err != nil {
			return nil, err
		}
	} else if err := grant.selection.Root.ValidateSelectionBounded(grant.selection.SelectedPath, recovery.MaximumPhysicalPathDepth, budget); err != nil {
		return nil, err
	}
	selected, err := rootedpath.NewRelativeDestination(destination.RelativePath())
	if err != nil {
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
	return grant.selection.Root.AcquireBounded(bound, recovery.MaximumPhysicalPathDepth, budget)
}

func acquireNativeOrdinaryGlobalWriter(entry recoveryEntry, destination output.Destination, acquire RootedCapabilityResolver, budget *recovery.PhysicalWorkBudget) (rootedpath.CommitCapability, error) {
	if entry.GlobalPathBinding == nil || acquire == nil {
		return nil, fmt.Errorf("ordinary global writer requires independently selected capability and stored binding")
	}
	capability, present, err := acquireMatchingRootedCapability(destination, entry.GlobalPathBinding.ResolvedPath, acquire, budget)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, fmt.Errorf("ordinary global writer has no selected capability")
	}
	expected, err := entry.GlobalPathBinding.RootProvenance.canonical()
	if err == nil {
		err = expected.Match(capability.Destination().Root())
	}
	if err != nil {
		return nil, errors.Join(err, capability.Close())
	}
	return capability, nil
}

func observeNativeWriterCapability(ctx context.Context, capability rootedpath.CommitCapability, budget *recovery.PhysicalWorkBudget) (observation mutation.DirectoryEntryAuthorityObservation, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, capability.Close(), ctx.Err())
		if resultErr != nil {
			observation = mutation.DirectoryEntryAuthorityObservation{}
		}
	}()
	if err := capability.Validate(); err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	observation, err = mutation.ObserveDirectoryEntryAuthorityBounded(path, recovery.MaximumPhysicalPathDepth, budget)
	return observation, errors.Join(err, capability.Validate())
}
