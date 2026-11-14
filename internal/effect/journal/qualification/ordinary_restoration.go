package qualification

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

type OrdinaryRestorationReadOptions struct {
	Manifest     *journalnamespace.ManifestAuthority
	Capabilities journalnamespace.CapabilityResolver
	Resolver     func(output.Destination) (string, error)
	Artifacts    *rootedpath.CapturedRoot
	Active       journalinventory.ActiveJournalAuthority
	Filesystem   mutationfs.RootedReader
	Codecs       aggregate.CodecCatalog
	Budget       *physicalwork.Budget
}

func ObserveRestorationOwner(statefile string, budget rootedpath.PhysicalTraversalBudget) (stateauthority.Key, error) {
	if budget == nil {
		return stateauthority.Key{}, fmt.Errorf("restoration owner observation requires its traversal budget")
	}
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(statefile, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return stateauthority.Key{}, err
	}
	exact, present := observed.Exact()
	if !present {
		return stateauthority.Key{}, fmt.Errorf("restoration requires an exact selected statefile namespace")
	}
	return stateauthority.NewKey(exact)
}

func RequireOrdinaryRestorationWriteExclusion(ctx context.Context, actions []recovery.Action, owner stateauthority.Key, registry ownership.Registry, options OrdinaryRestorationReadOptions) error {
	groups := make(map[string][]recovery.Action)
	var order []string
	for _, action := range actions {
		if action.Kind != recovery.ActionKindRestoreWrite && action.Kind != recovery.ActionKindRestoreDelete {
			continue
		}
		if ctx == nil || options.Artifacts == nil || options.Filesystem == nil || options.Budget == nil {
			return fmt.Errorf("ordinary restoration requires bounded selected read authority")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		capability, err := options.acquire(action)
		if err != nil {
			return err
		}
		path, pathErr := capability.Destination().LexicalPath()
		if err := errors.Join(pathErr, capability.Close()); err != nil {
			return err
		}
		if len(groups[path]) == 0 {
			order = append(order, path)
		}
		groups[path] = append(groups[path], action)
	}
	if len(order) == 0 {
		return nil
	}
	if err := registry.RequireWriteExclusion(owner, nil, nil, nil, nil); err != nil {
		return err
	}
	if err := journalinventory.ValidateActiveJournalRoot(ctx, options.Filesystem, options.Artifacts, options.Budget, options.Active); err != nil {
		return err
	}
	for _, path := range order {
		if err := ctx.Err(); err != nil {
			return err
		}
		effects, err := options.effects(ctx, groups[path])
		if err != nil {
			return fmt.Errorf("qualify restoration effects at %q: %w", path, err)
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, options.Budget)
		if err != nil {
			return err
		}
		if err := RequireRestorationEffectsWriteExclusion(owner, registry, observed, effects); err != nil {
			return fmt.Errorf("ordinary restoration ownership at %q: %w", path, err)
		}
	}
	return nil
}

func (options OrdinaryRestorationReadOptions) acquire(action recovery.Action) (rootedpath.CommitCapability, error) {
	destination, err := output.Parse(action.Destination)
	if err != nil {
		return nil, err
	}
	if err := destination.ValidateScope(action.Scope); err != nil {
		return nil, err
	}
	if action.Scope == target.ScopeProject {
		if options.Manifest == nil {
			return nil, fmt.Errorf("ordinary restoration requires selected project authority")
		}
		return options.Manifest.AcquireBounded(destination, options.Budget)
	}
	if options.Resolver == nil {
		return nil, fmt.Errorf("ordinary restoration requires an independent global resolver")
	}
	path, err := options.Resolver(destination)
	if err != nil {
		return nil, err
	}
	return acquireGlobalRecoveryCapability(destination, path, options.Capabilities, options.Budget)
}

func (options OrdinaryRestorationReadOptions) effects(ctx context.Context, actions []recovery.Action) (result recovery.RestorationEffects, resultErr error) {
	first := actions[0]
	capability, err := options.acquire(first)
	if err != nil {
		return recovery.RestorationEffects{}, err
	}
	defer func() {
		if err := capability.Close(); err != nil {
			result = recovery.RestorationEffects{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if first.ContentPath == "" {
		if len(actions) != 1 || first.AggregateContract != nil {
			return recovery.RestorationEffects{}, fmt.Errorf("ordinary restoration mixes whole and selected actions")
		}
		if first.Kind == recovery.ActionKindRestoreDelete || first.BackupKind == recovery.PathKindDirectory {
			_, err := options.Filesystem.CaptureRootedEntryIdentity(ctx, capability)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return recovery.RestorationEffects{}, err
			}
			return recovery.NewWholePathRestorationEffects(err == nil, first.Kind == recovery.ActionKindRestoreWrite)
		}
		current, mode, err := options.read(ctx, capability, recovery.MaximumRecoveryBackupFileBytes)
		if err != nil {
			return recovery.RestorationEffects{}, err
		}
		content, err := options.backup(ctx, first)
		if err != nil {
			return recovery.RestorationEffects{}, err
		}
		return recovery.NewFileRestorationEffects(current, mode, aggregate.ExistingDocument(content), first.BeforePathMode)
	}

	var contracts []aggregate.ProjectionContract
	var states []aggregate.ProjectionState
	fileMode := recovery.NewPermissionMode(aggregate.DocumentFileMode)
	if first.BeforePathExisted {
		fileMode = first.BeforePathMode
	}
	for _, action := range actions {
		if action.ContentPath == "" || action.AggregateContract == nil || action.BeforePathExisted != first.BeforePathExisted || action.BeforeParentExisted != first.BeforeParentExisted {
			return recovery.RestorationEffects{}, fmt.Errorf("ordinary selected restoration has inconsistent baseline facts")
		}
		if action.BeforePathExisted && (action.BeforePathMode == nil || fileMode == nil || *action.BeforePathMode != *fileMode) {
			return recovery.RestorationEffects{}, fmt.Errorf("ordinary selected restoration has inconsistent root mode")
		}
		contract := action.AggregateContract.Clone()
		var content []byte
		if action.Kind == recovery.ActionKindRestoreWrite {
			content, err = options.backup(ctx, action)
			if err != nil {
				return recovery.RestorationEffects{}, err
			}
		}
		state, err := aggregate.NewProjectionState(contract, action.BeforeParentExisted, action.Kind == recovery.ActionKindRestoreWrite, string(content))
		if err != nil {
			return recovery.RestorationEffects{}, err
		}
		contracts = append(contracts, contract)
		states = append(states, state)
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		return recovery.RestorationEffects{}, err
	}
	baseline, err := aggregate.NewSnapshot(first.BeforePathExisted, selection, states)
	if err != nil {
		return recovery.RestorationEffects{}, err
	}
	codec, present := options.Codecs.Lookup(selection.CodecContractID())
	if !present {
		return recovery.RestorationEffects{}, fmt.Errorf("ordinary restoration codec is unavailable")
	}
	current, mode, err := options.read(ctx, capability, codec.MaximumDocumentBytes())
	if err != nil {
		return recovery.RestorationEffects{}, err
	}
	_, effects, err := recovery.RestoreProjectionDocument(current, mode, baseline, fileMode, options.Codecs)
	return effects, err
}

func (options OrdinaryRestorationReadOptions) backup(ctx context.Context, action recovery.Action) (result []byte, resultErr error) {
	if action.BackupKind != recovery.PathKindFile {
		return nil, fmt.Errorf("ordinary restoration requires a regular backup")
	}
	authority, err := options.Artifacts.AuthorityBounded(options.Budget)
	if err != nil {
		return nil, err
	}
	relative, err := rootedpath.NewRelativeDestination(action.BackupPath)
	if err != nil {
		return nil, err
	}
	bound, err := authority.Bind(relative)
	if err != nil {
		return nil, err
	}
	capability, err := options.Artifacts.AcquireBounded(bound, physicalwork.MaximumPhysicalPathDepth, options.Budget)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := capability.Close(); err != nil {
			result = nil
			resultErr = errors.Join(resultErr, err)
		}
	}()
	document, mode, err := options.read(ctx, capability, max(int64(1), action.BackupWork.Bytes()))
	if err != nil {
		return nil, err
	}
	if !document.Exists() || mode == nil || document.ContentSize() != action.BackupWork.Bytes() || string(artifact.HashFileContentWithExecutable(document.Content(), mode.FileMode()&0o111 != 0)) != action.BackupHash {
		return nil, fmt.Errorf("ordinary restoration backup changed")
	}
	return document.Content(), nil
}

func (options OrdinaryRestorationReadOptions) read(ctx context.Context, capability rootedpath.CommitCapability, maximumBytes int64) (aggregate.Document, *recovery.PermissionMode, error) {
	maximum, capacity, err := recoveryArtifactWorkLimits(false, maximumBytes, options.Budget)
	if err != nil {
		return aggregate.Document{}, nil, err
	}
	content, mode, _, err := options.Filesystem.ReadRootedRegularFileUpTo(ctx, capability, capacity.Bytes())
	if errors.Is(err, fs.ErrNotExist) {
		return aggregate.AbsentDocument(), nil, nil
	}
	if err != nil {
		return aggregate.Document{}, nil, errors.Join(err, options.Budget.AdmitIndeterminateTreeWork(maximum, capacity))
	}
	work, err := workmeasure.NewArtifactWork(0, int64(len(content)))
	if err != nil {
		return aggregate.Document{}, nil, err
	}
	if err := options.Budget.AdmitTreeWithin(work, maximum); err != nil {
		return aggregate.Document{}, nil, err
	}
	return aggregate.ExistingDocument(content), recovery.NewPermissionMode(mode), nil
}
