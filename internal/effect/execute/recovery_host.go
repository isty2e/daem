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
	BeforePathMode      *recovery.PermissionMode
	BeforePathExisted   bool
	BeforeParentExisted bool
	ExpectedAfter       recovery.ExpectedPathState
	AggregateContract   *aggregate.ProjectionContract
}

type recoveryHostOwnershipGuard func(context.Context, recoveryHostAction, mutationDestination) error

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
	actions []recovery.Action,
	resolver DestinationResolver,
	filesystem mutationfs.Store,
	ownershipRegistryBinder ownershipmutation.RootedRegistryBinder,
) (*mutationAuthority, error) {
	authority, err := captureMutationAuthority(paths, true, nil, resolver, filesystem)
	if err != nil {
		return nil, err
	}
	authority.ownershipRegistryBinder = ownershipRegistryBinder
	for _, action := range actions {
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
	operationDir string,
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
	for index, action := range actions {
		if err := action.validateAggregateCorrelation(); err != nil {
			return fmt.Errorf("recovery action %d: %w", index, err)
		}
	}
	expectedByDestination := make(map[string]recoveryWholePathState, len(actions))
	for index, action := range actions {
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

		logical, err := recoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return err
		}
		destination, err := authority.resolveBoundDestination(action.Scope, logical)
		if err != nil {
			return err
		}
		if ownershipGuard != nil {
			if err := ownershipGuard(ctx, action, destination); err != nil {
				return fmt.Errorf("validate recovery host action %d ownership: %w", index, err)
			}
		}
		destinationKey := filepath.Clean(destination.hostPath)
		expectedWholeState, present := expectedByDestination[destinationKey]
		if !present {
			expectedWholeState = staged[index].stagedState
		}

		switch action.Kind {
		case recovery.ActionKindRestoreWrite:
			backup, err := recoveryBackupForAction(operationDir, action)
			if err != nil {
				return fmt.Errorf("open backup for %q: %w", action.Destination, err)
			}

			if action.ContentPath != "" {
				content, err := backup.readFile(ctx)
				if err != nil {
					return fmt.Errorf("read projection backup for %q content path %q: %w", action.Destination, action.ContentPath, err)
				}
				staged[index].attempted = true
				effectState, effectKnown, err := restoreAggregateProjection(
					ctx,
					authority,
					destination,
					action,
					content,
					true,
					&expectedWholeState,
					codecs,
				)
				staged[index].effectState = effectState
				staged[index].effectKnown = effectKnown
				if err != nil {
					return fmt.Errorf("restore aggregate projection %q content path %q: %w", action.Destination, action.ContentPath, err)
				}
				if !effectKnown {
					return fmt.Errorf("restore aggregate projection %q did not produce an exact whole-document postcondition", action.Destination)
				}
				expectedByDestination[destinationKey] = effectState
				if err := gate.acceptAfter(ctx); err != nil {
					return fmt.Errorf("accept recovery host action %d visibility: %w", index, err)
				}
				continue
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
			if action.ContentPath != "" {
				staged[index].attempted = true
				effectState, effectKnown, err := restoreAggregateProjection(
					ctx,
					authority,
					destination,
					action,
					nil,
					false,
					&expectedWholeState,
					codecs,
				)
				staged[index].effectState = effectState
				staged[index].effectKnown = effectKnown
				if err != nil {
					return fmt.Errorf("remove aggregate projection %q content path %q: %w", action.Destination, action.ContentPath, err)
				}
				if !effectKnown {
					return fmt.Errorf("remove aggregate projection %q did not produce an exact whole-document postcondition", action.Destination)
				}
				expectedByDestination[destinationKey] = effectState
				if err := gate.acceptAfter(ctx); err != nil {
					return fmt.Errorf("accept recovery host action %d visibility: %w", index, err)
				}
				continue
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
		expectedByDestination[destinationKey] = staged[index].effectState
		if err := gate.acceptAfter(ctx); err != nil {
			return fmt.Errorf("accept recovery host action %d visibility: %w", index, err)
		}
	}

	return nil
}

func recoveryBackupForAction(
	operationDir string,
	action recoveryHostAction,
) (recoveryBackup, error) {
	backupPath := filepath.Join(operationDir, filepath.FromSlash(action.BackupPath))
	return newRecoveryBackup(
		backupPath,
		action.BackupPath,
		action.BackupKind,
		action.BackupHash,
	)
}

func requiredBeforeFileMode(action recoveryHostAction) (os.FileMode, error) {
	if action.BeforePathMode == nil {
		return 0, fmt.Errorf("recovery action for %q requires before path mode", action.Destination)
	}
	return action.BeforePathMode.FileMode(), nil
}

func restoreAggregateProjection(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
	action recoveryHostAction,
	projection []byte,
	projectionPresent bool,
	expectedWholeState *recoveryWholePathState,
	codecs aggregate.CodecCatalog,
) (recoveryWholePathState, bool, error) {
	contract := action.AggregateContract.Clone()
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		return recoveryWholePathState{}, false, err
	}
	baselineState, err := aggregate.NewProjectionState(
		contract,
		action.BeforeParentExisted,
		projectionPresent,
		string(projection),
	)
	if err != nil {
		return recoveryWholePathState{}, false, err
	}
	baseline, err := aggregate.NewSnapshot(action.BeforePathExisted, selection, []aggregate.ProjectionState{baselineState})
	if err != nil {
		return recoveryWholePathState{}, false, err
	}
	codec, ok := codecs.Lookup(contract.CodecContractID())
	if !ok {
		return recoveryWholePathState{}, false, fmt.Errorf("aggregate recovery codec %q is not admitted", contract.CodecContractID())
	}
	fileMode := aggregate.DocumentFileMode
	if action.BeforePathExisted && action.BeforePathMode != nil {
		fileMode = action.BeforePathMode.FileMode()
	}
	var expected aggregate.RenderedDocument
	expectedKnown := false
	outcome := mutateFileDestinationWithOutcome(
		ctx,
		authority,
		destination,
		fileMode,
		!action.ExpectedAfter.PathExisted,
		codec.MaximumDocumentBytes(),
		func(existing []byte, mode os.FileMode, exists bool) ([]byte, bool, error) {
			if expectedWholeState != nil {
				if err := validateRecoveryWholeFileInput(
					action.Destination,
					*expectedWholeState,
					existing,
					mode,
					exists,
				); err != nil {
					return nil, true, err
				}
			}
			if err := validateRecoveryExpectedContentPath(action, existing, mode, exists, codecs); err != nil {
				return nil, true, err
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
			return recoveryDocumentWholeState(expected.Document(), fileMode), true, outcome.err
		}
		return recoveryWholePathState{}, false, outcome.err
	}
	current, mode, err := readAggregateDocumentDestination(
		ctx,
		authority,
		destination,
		codec.MaximumDocumentBytes(),
	)
	if err != nil {
		return recoveryDocumentWholeState(expected.Document(), fileMode), true, err
	}
	if !current.Equal(expected.Document()) {
		return recoveryDocumentWholeState(expected.Document(), fileMode), true, fmt.Errorf("aggregate recovery document postcondition failed")
	}
	if current.Exists() && mode.Perm() != fileMode.Perm() {
		return recoveryDocumentWholeState(expected.Document(), fileMode), true, fmt.Errorf(
			"aggregate recovery mode = %04o, want %04o",
			mode.Perm(),
			fileMode.Perm(),
		)
	}
	postSnapshot, failure := codec.Read(current, selection)
	if failure != nil {
		return recoveryDocumentWholeState(expected.Document(), fileMode), true, failure
	}
	if !postSnapshot.Equal(expected.Expected()) {
		return recoveryDocumentWholeState(expected.Document(), fileMode), true, fmt.Errorf("aggregate recovery selected postcondition failed")
	}
	return recoveryDocumentWholeState(expected.Document(), fileMode), true, nil
}
