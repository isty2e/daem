package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (authority *mutationAuthority) prepareRecoveryForwardRemovals(
	ctx context.Context,
	actions []recoveryHostAction,
	codecs aggregate.CodecCatalog,
) error {
	if authority == nil || !authority.removalBindingsPrepared || authority.physicalWorkBudget == nil {
		return fmt.Errorf("recovery forward removal requires prepared physical bindings")
	}
	if authority.forwardRemovalPrepared {
		if authority.forwardRemovalExecution == nil {
			return fmt.Errorf("recovery forward removal preparation is incomplete")
		}
		return nil
	}
	certificates, err := recoveryForwardRemovalCertificates(
		ctx,
		actions,
		authority.removalDemands,
		codecs,
	)
	if err != nil {
		return err
	}
	return authority.prepareForwardRemovalReservations(ctx, certificates)
}

func recoveryForwardRemovalCertificates(
	ctx context.Context,
	actions []recoveryHostAction,
	demands recovery.RemovalDemandSet,
	codecs aggregate.CodecCatalog,
) ([]forwardRemovalCertificate, error) {
	certificates := make([]forwardRemovalCertificate, 0, len(actions))
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
		relation := removalRelationKey{scope: action.Scope, destination: destination}
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
	values []forwardRemovalCertificate,
	candidate forwardRemovalCertificate,
) []forwardRemovalCertificate {
	for _, value := range values {
		if value.relation == candidate.relation && value.state.Equal(candidate.state) {
			return values
		}
	}
	return append(values, candidate)
}

func removalStatesForRelation(
	demands recovery.RemovalDemandSet,
	relation removalRelationKey,
) []recovery.RemovalState {
	for _, demand := range demands.Demands() {
		if demand.Scope() == relation.scope && demand.Destination() == relation.destination {
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
