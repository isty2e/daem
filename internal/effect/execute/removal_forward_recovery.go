package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/removaloperation"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func prepareRecoveryForwardRemovals(
	ctx context.Context,
	removals *removaloperation.Operation,
	actions []recoveryHostAction,
	codecs aggregate.CodecCatalog,
) error {
	return removals.PrepareRecoveryForward(
		ctx,
		func(demands recovery.RemovalDemandSet) ([]removaloperation.ForwardCandidate, error) {
			return recoveryForwardRemovalCertificates(ctx, actions, demands, codecs)
		},
	)
}

func recoveryForwardRemovalCertificates(
	ctx context.Context,
	actions []recoveryHostAction,
	demands recovery.RemovalDemandSet,
	codecs aggregate.CodecCatalog,
) ([]removaloperation.ForwardCandidate, error) {
	certificates := make([]removaloperation.ForwardCandidate, 0, len(actions))
	for index, action := range actions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if action.Kind != recovery.ActionKindRestoreWrite {
			continue
		}
		destination, err := recoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return nil, fmt.Errorf("recovery action[%d] forward removal destination: %w", index, err)
		}
		relation := removaloperation.Relation{Scope: action.Scope, Destination: destination}
		states := removalStatesForRelation(demands, relation)
		if len(states) == 0 {
			continue
		}

		if action.ContentPath != "" {
			state, found, err := uniqueExistingBeforeRemovalState(states)
			if err != nil {
				return nil, fmt.Errorf("recovery action[%d] aggregate removal state: %w", index, err)
			}
			if !found {
				continue
			}
			if action.AggregateContract == nil {
				return nil, fmt.Errorf("recovery action[%d] aggregate contract is required", index)
			}
			codec, admitted := codecs.Lookup(action.AggregateContract.CodecContractID())
			if !admitted {
				return nil, fmt.Errorf(
					"recovery action[%d] aggregate codec contract %q is not admitted",
					index,
					action.AggregateContract.CodecContractID(),
				)
			}
			certificates = appendUniqueForwardRemovalCertificate(
				certificates,
				constantForwardRemovalCertificate(relation, state, 0, codec.MaximumDocumentBytes()),
			)
			continue
		}

		state, err := recoveryActionBeforeRemovalState(action)
		if err != nil {
			return nil, fmt.Errorf("recovery action[%d] before removal state: %w", index, err)
		}
		if !removalStateIsDemanded(demands, relation, state) {
			continue
		}
		switch action.BackupKind {
		case recovery.PathKindFile, recovery.PathKindDirectory:
			certificates = appendUniqueForwardRemovalCertificate(
				certificates,
				constantForwardRemovalCertificate(
					relation,
					state,
					action.BackupWork.Entries(),
					action.BackupWork.Bytes(),
				),
			)
		default:
			return nil, fmt.Errorf(
				"recovery action[%d] backup kind %q cannot produce a removable regular entry",
				index,
				action.BackupKind,
			)
		}
	}
	return certificates, nil
}

func appendUniqueForwardRemovalCertificate(
	values []removaloperation.ForwardCandidate,
	candidate removaloperation.ForwardCandidate,
) []removaloperation.ForwardCandidate {
	for _, value := range values {
		if value.Relation == candidate.Relation && value.State.Equal(candidate.State) {
			return values
		}
	}
	return append(values, candidate)
}

func removalStatesForRelation(
	demands recovery.RemovalDemandSet,
	relation removaloperation.Relation,
) []recovery.RemovalState {
	for _, demand := range demands.Demands() {
		if demand.Scope() == relation.Scope && demand.Destination() == relation.Destination {
			return demand.States()
		}
	}
	return nil
}

func uniqueExistingBeforeRemovalState(
	states []recovery.RemovalState,
) (recovery.RemovalState, bool, error) {
	var result recovery.RemovalState
	found := false
	for _, state := range states {
		before, present := state.Before()
		if !present || !before.Existed {
			continue
		}
		if found {
			return recovery.RemovalState{}, false, fmt.Errorf(
				"removal relation has multiple existing before states",
			)
		}
		result = state
		found = true
	}
	return result, found, nil
}

func recoveryActionBeforeRemovalState(action recoveryHostAction) (recovery.RemovalState, error) {
	state := recovery.BeforePathState{
		Existed:       true,
		PathExisted:   action.BeforePathExisted,
		ParentExisted: action.BeforeParentExisted,
		Kind:          action.BackupKind,
		ContentHash:   action.BackupHash,
	}
	if action.BackupKind == recovery.PathKindFile {
		if action.BeforePathMode == nil {
			return recovery.RemovalState{}, fmt.Errorf("file backup requires before path mode")
		}
		state.PathMode = recovery.NewPermissionMode(action.BeforePathMode.FileMode())
	}
	return recovery.NewBeforeRemovalState(state)
}
