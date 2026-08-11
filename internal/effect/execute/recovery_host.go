package execute

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type recoveryHostAction struct {
	Kind                recovery.ActionKind
	Scope               target.Scope
	Destination         string
	ContentPath         string
	BackupPath          string
	BackupHash          string
	BackupKind          string
	BackupWork          recovery.ArtifactWork
	BeforePathMode      *recovery.PermissionMode
	BeforePathExisted   bool
	BeforeParentExisted bool
	ExpectedAfter       recovery.ExpectedPathState
	AggregateContract   *aggregate.ProjectionContract
}

type recoveryHostOwnershipGuard func(context.Context, recoveryHostAction, mutationDestination) error

type recoveryAggregateActionGroup struct {
	indexes     []int
	destination mutationDestination
}

func recoveryDestination(scope target.Scope, value string) (output.Destination, error) {
	destination, err := output.Parse(value)
	if err != nil {
		return output.Destination{}, fmt.Errorf("recovery destination: %w", err)
	}
	if err := destination.ValidateScope(scope); err != nil {
		return output.Destination{}, fmt.Errorf("recovery destination: %w", err)
	}
	return destination, nil
}

func (action recoveryHostAction) validateAggregateCorrelation() error {
	if action.ContentPath == "" {
		if action.AggregateContract != nil {
			return fmt.Errorf("aggregate contract has no content path")
		}
		return nil
	}
	if action.AggregateContract == nil {
		return fmt.Errorf("content path %q has no aggregate contract", action.ContentPath)
	}
	contract := action.AggregateContract.Clone()
	if err := contract.Validate(); err != nil {
		return fmt.Errorf("aggregate contract: %w", err)
	}
	address := contract.Address()
	document := address.Document()
	if action.Scope != document.Scope() {
		return fmt.Errorf(
			"scope %q does not match aggregate contract scope %q",
			action.Scope,
			document.Scope(),
		)
	}
	if action.Destination != document.AggregateRoot().String() ||
		action.ContentPath != string(address.ContentPath()) {
		return fmt.Errorf(
			"aggregate address %q%q does not match contract address %q%q",
			action.Destination,
			action.ContentPath,
			document.AggregateRoot(),
			address.ContentPath(),
		)
	}
	return nil
}

// orderRecoveryHostActions restores providers before switching dependent
// projections and removes newly published paths only after those projections
// no longer reference them. Canonical journal order remains stable within each
// phase.
func orderRecoveryHostActions(actions []recoveryHostAction) []recoveryHostAction {
	pathRestores := make([]recoveryHostAction, 0, len(actions))
	projectionRestores := make([]recoveryHostAction, 0, len(actions))
	pathDeletes := make([]recoveryHostAction, 0, len(actions))
	for _, action := range actions {
		switch {
		case action.ContentPath != "" || action.AggregateContract != nil:
			projectionRestores = append(projectionRestores, action)
		case action.Kind == recovery.ActionKindRestoreWrite:
			pathRestores = append(pathRestores, action)
		default:
			pathDeletes = append(pathDeletes, action)
		}
	}
	ordered := make([]recoveryHostAction, 0, len(actions))
	ordered = append(ordered, pathRestores...)
	ordered = append(ordered, projectionRestores...)
	ordered = append(ordered, pathDeletes...)
	return ordered
}

func newRecoveryMutationAuthority(
	paths Paths,
	plan recovery.Plan,
	resolver DestinationResolver,
	filesystem mutationfs.Store,
	ownershipRegistryBinder ownershipmutation.RootedRegistryBinder,
) (*mutationAuthority, error) {
	demands, err := removalDemandSetFromIntents(plan.RemovalIntents())
	if err != nil {
		return nil, err
	}
	physicalWorkBudget, err := recovery.NewPhysicalWorkBudget(demands.Len())
	if err != nil {
		return nil, err
	}
	authority, err := captureMutationAuthorityWithPhysicalWorkBudget(
		paths,
		true,
		nil,
		resolver,
		filesystem,
		physicalWorkBudget,
	)
	if err != nil {
		return nil, err
	}
	authority.ownershipRegistryBinder = ownershipRegistryBinder
	if err := authority.prepareRemovalDemands(demands, physicalWorkBudget); err != nil {
		_ = authority.close()
		return nil, err
	}
	for _, action := range plan.GuardedActions() {
		destination, err := recoveryDestination(action.Scope, action.Destination)
		if err != nil {
			_ = authority.close()
			return nil, err
		}
		targets := append([]target.Target(nil), action.ConsumerTargets...)
		if action.Target != "" {
			targets = []target.Target{action.Target}
		}
		if err := authority.bindPhysicalAuthority(
			action.Scope,
			destination,
			targets,
		); err != nil {
			_ = authority.close()
			return nil, err
		}
	}
	return authority, nil
}

func requireRecoveryGlobalBindings(authority *mutationAuthority, actions []recovery.Action) error {
	if authority == nil {
		return fmt.Errorf("recovery mutation authority is required")
	}
	for _, action := range actions {
		if action.Scope != target.ScopeGlobal {
			continue
		}
		destination, err := recoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return err
		}
		if _, present := authority.globalDestinationBindings[destination]; !present {
			return fmt.Errorf("global recovery destination %q was not bound before effects", action.Destination)
		}
	}
	return nil
}

func executeRecoveryHostActions(
	ctx context.Context,
	authority *mutationAuthority,
	actions []recoveryHostAction,
	staged []hostRollbackEntry,
	beforeAction func(int) error,
	ownershipGuard recoveryHostOwnershipGuard,
	codecs aggregate.CodecCatalog,
	gate visibilityEffectGate,
) error {
	if len(actions) != len(staged) {
		return fmt.Errorf("recovery action count %d does not match staged precondition count %d", len(actions), len(staged))
	}
	resolvedDestinations := make([]mutationDestination, len(actions))
	aggregateGroups := make(map[int]recoveryAggregateActionGroup)
	aggregateGroupByDestination := make(map[string]int)
	aggregateMember := make(map[int]struct{})
	for index, action := range actions {
		if err := action.validateAggregateCorrelation(); err != nil {
			return fmt.Errorf("recovery action %d: %w", index, err)
		}
		logical, err := recoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return err
		}
		destination, err := authority.resolveBoundDestination(action.Scope, logical)
		if err != nil {
			return err
		}
		resolvedDestinations[index] = destination
		if action.ContentPath == "" {
			continue
		}
		destinationKey := filepath.Clean(destination.hostPath)
		firstIndex, present := aggregateGroupByDestination[destinationKey]
		if !present {
			firstIndex = index
			aggregateGroupByDestination[destinationKey] = firstIndex
			aggregateGroups[firstIndex] = recoveryAggregateActionGroup{destination: destination}
		}
		group := aggregateGroups[firstIndex]
		group.indexes = append(group.indexes, index)
		aggregateGroups[firstIndex] = group
		aggregateMember[index] = struct{}{}
	}
	for index, action := range actions {
		if group, present := aggregateGroups[index]; present {
			_, err := executeRecoveryAggregateActionGroup(
				ctx,
				authority,
				actions,
				staged,
				group,
				beforeAction,
				ownershipGuard,
				codecs,
				gate,
			)
			if err != nil {
				return err
			}
			continue
		}
		if _, grouped := aggregateMember[index]; grouped {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if beforeAction != nil {
			if err := beforeAction(index); err != nil {
				return fmt.Errorf("before recovery host action %d: %w", index, err)
			}
		}
		if err := gate.validateBefore(ctx); err != nil {
			return fmt.Errorf("validate recovery host action %d authority: %w", index, err)
		}

		destination := resolvedDestinations[index]
		if ownershipGuard != nil {
			if err := ownershipGuard(ctx, action, destination); err != nil {
				return fmt.Errorf("validate recovery host action %d ownership: %w", index, err)
			}
		}
		if err := validateStagedRecoveryDestination(ctx, authority, staged[index]); err != nil {
			return fmt.Errorf(
				"validate recovery host action %d baseline: %w",
				index,
				err,
			)
		}
		switch action.Kind {
		case recovery.ActionKindRestoreWrite:
			backup, err := authority.recoveryBackupForAction(action)
			if err != nil {
				return fmt.Errorf("open backup for %q: %w", action.Destination, err)
			}

			switch action.BackupKind {
			case recovery.PathKindFile:
				fileMode, err := requiredBeforeFileMode(action)
				if err != nil {
					return err
				}
				content, err := backup.readFile(ctx)
				if err != nil {
					return fmt.Errorf("read backup for %q: %w", action.Destination, err)
				}
				if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
					return fmt.Errorf("validate recovery host action %d semantics: %w", index, err)
				}
				staged[index].attempted = true
				staged[index].effectState = recoveryWholePathState{
					existed: true, kind: recovery.PathKindFile,
					contentHash: action.BackupHash, fileMode: fileMode.Perm(),
				}
				staged[index].effectKnown = true
				if err := commitFileDestinationAgainst(
					ctx,
					authority,
					destination,
					content,
					fileMode,
					staged[index].existed,
					staged[index].identity,
				); err != nil {
					return fmt.Errorf("restore file %q: %w", action.Destination, err)
				}
			case recovery.PathKindDirectory:
				if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
					return fmt.Errorf("validate recovery host action %d semantics: %w", index, err)
				}
				staged[index].attempted = true
				staged[index].effectState = recoveryWholePathState{
					existed: true, kind: recovery.PathKindDirectory, contentHash: action.BackupHash,
				}
				staged[index].effectKnown = true
				if err := commitRecoveryDirectoryDestinationAgainst(
					ctx,
					authority,
					backup,
					destination,
					staged[index].existed,
					staged[index].identity,
				); err != nil {
					return fmt.Errorf("restore directory %q: %w", action.Destination, err)
				}
			default:
				return fmt.Errorf("backup kind %q for %q is not supported", action.BackupKind, action.Destination)
			}
		case recovery.ActionKindRestoreDelete:
			if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
				return fmt.Errorf("validate recovery host action %d semantics: %w", index, err)
			}
			staged[index].attempted = true
			staged[index].effectState = recoveryWholePathState{}
			staged[index].effectKnown = true
			if err := removeDestinationAgainst(ctx, authority, destination, staged[index].identity); err != nil {
				return fmt.Errorf("delete restored path %q: %w", action.Destination, err)
			}
		}
		if !staged[index].effectKnown {
			return fmt.Errorf("recovery action for %q did not produce an exact whole-path postcondition", action.Destination)
		}
		if err := gate.acceptAfter(ctx); err != nil {
			return fmt.Errorf("accept recovery host action %d visibility: %w", index, err)
		}
	}

	return nil
}

func requiredBeforeFileMode(action recoveryHostAction) (os.FileMode, error) {
	if action.BeforePathMode == nil {
		return 0, fmt.Errorf("recovery action for %q requires before path mode", action.Destination)
	}
	return action.BeforePathMode.FileMode(), nil
}

func executeRecoveryAggregateActionGroup(
	ctx context.Context,
	authority *mutationAuthority,
	actions []recoveryHostAction,
	staged []hostRollbackEntry,
	group recoveryAggregateActionGroup,
	beforeAction func(int) error,
	ownershipGuard recoveryHostOwnershipGuard,
	codecs aggregate.CodecCatalog,
	gate visibilityEffectGate,
) (recoveryWholePathState, error) {
	if len(group.indexes) == 0 {
		return recoveryWholePathState{}, fmt.Errorf("aggregate recovery group is empty")
	}
	firstIndex := group.indexes[0]
	firstAction := actions[firstIndex]
	baselineWholeState := staged[firstIndex].stagedState
	contracts := make([]aggregate.ProjectionContract, 0, len(group.indexes))
	states := make([]aggregate.ProjectionState, 0, len(group.indexes))
	for _, index := range group.indexes {
		if err := ctx.Err(); err != nil {
			return recoveryWholePathState{}, err
		}
		if beforeAction != nil {
			if err := beforeAction(index); err != nil {
				return recoveryWholePathState{}, fmt.Errorf("before recovery host action %d: %w", index, err)
			}
		}
		action := actions[index]
		if !staged[index].stagedState.equal(baselineWholeState) ||
			staged[index].existed != staged[firstIndex].existed {
			return recoveryWholePathState{}, fmt.Errorf(
				"aggregate recovery group for %q has inconsistent whole-document preconditions",
				firstAction.Destination,
			)
		}
		if staged[index].existed &&
			(staged[index].identity == nil || staged[firstIndex].identity == nil ||
				!staged[index].identity.Equal(staged[firstIndex].identity)) {
			return recoveryWholePathState{}, fmt.Errorf(
				"aggregate recovery group for %q has inconsistent document identity",
				firstAction.Destination,
			)
		}
		if action.BeforePathExisted != firstAction.BeforePathExisted ||
			action.BeforeParentExisted != firstAction.BeforeParentExisted ||
			action.ExpectedAfter.PathExisted != firstAction.ExpectedAfter.PathExisted {
			return recoveryWholePathState{}, fmt.Errorf(
				"aggregate recovery group for %q has inconsistent document facts",
				firstAction.Destination,
			)
		}
		contract := action.AggregateContract.Clone()
		projectionPresent := action.Kind == recovery.ActionKindRestoreWrite
		var projection []byte
		switch action.Kind {
		case recovery.ActionKindRestoreWrite:
			backup, err := authority.recoveryBackupForAction(action)
			if err != nil {
				return recoveryWholePathState{}, fmt.Errorf(
					"open projection backup for %q content path %q: %w",
					action.Destination,
					action.ContentPath,
					err,
				)
			}
			projection, err = backup.readFile(ctx)
			if err != nil {
				return recoveryWholePathState{}, fmt.Errorf(
					"read projection backup for %q content path %q: %w",
					action.Destination,
					action.ContentPath,
					err,
				)
			}
		case recovery.ActionKindRestoreDelete:
		default:
			return recoveryWholePathState{}, fmt.Errorf(
				"aggregate recovery action %d has unsupported kind %q",
				index,
				action.Kind,
			)
		}
		state, err := aggregate.NewProjectionState(
			contract,
			action.BeforeParentExisted,
			projectionPresent,
			string(projection),
		)
		if err != nil {
			return recoveryWholePathState{}, fmt.Errorf("aggregate recovery action %d baseline: %w", index, err)
		}
		contracts = append(contracts, contract)
		states = append(states, state)
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		return recoveryWholePathState{}, fmt.Errorf("aggregate recovery selection: %w", err)
	}
	baseline, err := aggregate.NewSnapshot(firstAction.BeforePathExisted, selection, states)
	if err != nil {
		return recoveryWholePathState{}, fmt.Errorf("aggregate recovery baseline: %w", err)
	}
	codec, ok := codecs.Lookup(selection.CodecContractID())
	if !ok {
		return recoveryWholePathState{}, fmt.Errorf("aggregate recovery codec %q is not admitted", selection.CodecContractID())
	}
	fileMode := aggregate.DocumentFileMode
	if firstAction.BeforePathExisted {
		fileMode, err = requiredBeforeFileMode(firstAction)
		if err != nil {
			return recoveryWholePathState{}, err
		}
	}
	for _, index := range group.indexes[1:] {
		action := actions[index]
		if action.BeforePathExisted &&
			(action.BeforePathMode == nil || action.BeforePathMode.FileMode().Perm() != fileMode.Perm()) {
			return recoveryWholePathState{}, fmt.Errorf(
				"aggregate recovery group for %q has inconsistent document mode",
				firstAction.Destination,
			)
		}
	}
	if err := gate.validateBefore(ctx); err != nil {
		return recoveryWholePathState{}, fmt.Errorf(
			"validate aggregate recovery group at action %d authority: %w",
			firstIndex,
			err,
		)
	}
	if ownershipGuard != nil {
		for _, index := range group.indexes {
			if err := ownershipGuard(ctx, actions[index], group.destination); err != nil {
				return recoveryWholePathState{}, fmt.Errorf("validate recovery host action %d ownership: %w", index, err)
			}
		}
	}
	for _, index := range group.indexes {
		staged[index].attempted = true
	}
	if err := admitRecoveryObservation(
		authority.generalExecutionWorkBudget,
		recovery.PathKindFile,
		staged[firstIndex].stagedWork,
	); err != nil {
		return recoveryWholePathState{}, fmt.Errorf(
			"admit aggregate recovery baseline observation: %w",
			err,
		)
	}
	if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
		return recoveryWholePathState{}, fmt.Errorf(
			"validate aggregate recovery group at action %d semantics: %w",
			firstIndex,
			err,
		)
	}
	var expected aggregate.RenderedDocument
	expectedKnown := false
	outcome := mutateFileDestinationWithOutcome(
		ctx,
		authority,
		group.destination,
		fileMode,
		!firstAction.ExpectedAfter.PathExisted,
		max(int64(1), staged[firstIndex].stagedWork.Bytes()),
		func(existing []byte, mode os.FileMode, exists bool) ([]byte, bool, error) {
			if err := validateRecoveryWholeFileInput(
				firstAction.Destination,
				baselineWholeState,
				existing,
				mode,
				exists,
			); err != nil {
				return nil, true, err
			}
			for _, index := range group.indexes {
				if err := validateRecoveryExpectedContentPath(actions[index], existing, mode, exists, codecs); err != nil {
					return nil, true, err
				}
			}
			current := aggregate.AbsentDocument()
			if exists {
				current = aggregate.ExistingDocument(existing)
			}
			rendered, failure := codec.Restore(current, baseline)
			if failure != nil {
				return nil, true, failure
			}
			expected = rendered
			expectedKnown = true
			candidate := expected.Document()
			return candidate.Content(), candidate.Exists(), nil
		},
	)
	if outcome.err != nil {
		if expectedKnown {
			effectState := recoveryDocumentWholeState(expected.Document(), fileMode)
			for _, index := range group.indexes {
				staged[index].effectState = effectState
				staged[index].effectKnown = true
			}
			return effectState, outcome.err
		}
		return recoveryWholePathState{}, outcome.err
	}
	expectedWork, err := recovery.NewArtifactWork(0, int64(len(expected.Document().Content())))
	if err != nil {
		return recoveryWholePathState{}, err
	}
	if err := admitRecoveryObservation(
		authority.generalExecutionWorkBudget,
		recovery.PathKindFile,
		expectedWork,
	); err != nil {
		return recoveryWholePathState{}, fmt.Errorf(
			"admit aggregate recovery postcondition observation: %w",
			err,
		)
	}
	current, mode, err := readAggregateDocumentDestination(
		ctx,
		authority,
		group.destination,
		max(int64(1), expectedWork.Bytes()),
	)
	effectState := recoveryDocumentWholeState(expected.Document(), fileMode)
	for _, index := range group.indexes {
		staged[index].effectState = effectState
		staged[index].effectKnown = true
	}
	if err != nil {
		return effectState, err
	}
	if !current.Equal(expected.Document()) {
		return effectState, fmt.Errorf("aggregate recovery document postcondition failed")
	}
	if current.Exists() && mode.Perm() != fileMode.Perm() {
		return effectState, fmt.Errorf(
			"aggregate recovery mode = %04o, want %04o",
			mode.Perm(),
			fileMode.Perm(),
		)
	}
	postSnapshot, failure := codec.Read(current, selection)
	if failure != nil {
		return effectState, failure
	}
	if !postSnapshot.Equal(expected.Expected()) {
		return effectState, fmt.Errorf("aggregate recovery selected postcondition failed")
	}
	if err := gate.acceptAfter(ctx); err != nil {
		return effectState, fmt.Errorf(
			"accept aggregate recovery group at action %d visibility: %w",
			firstIndex,
			err,
		)
	}
	return effectState, nil
}
