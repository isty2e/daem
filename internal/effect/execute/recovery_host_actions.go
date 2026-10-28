package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func recoveryHostActionsFor(actions []recovery.Action) ([]recoveryHostAction, error) {
	hostActions := make([]recoveryHostAction, 0, len(actions))
	for _, action := range actions {
		switch action.Kind {
		case recovery.ActionKindNoOp:
			continue
		case recovery.ActionKindRestoreWrite:
			hostActions = append(hostActions, recoveryHostAction{
				Kind: action.Kind, Scope: action.Scope, Destination: action.Destination,
				ContentPath: action.ContentPath, BackupPath: action.BackupPath,
				BackupHash: action.BackupHash, BackupKind: action.BackupKind, BackupWork: action.BackupWork,
				BeforePathMode: action.BeforePathMode, BeforePathExisted: action.BeforePathExisted,
				BeforeParentExisted: action.BeforeParentExisted, ExpectedAfter: action.ExpectedRestorationInput(),
				AggregateContract: action.AggregateContract,
			})
		case recovery.ActionKindRestoreDelete:
			hostActions = append(hostActions, recoveryHostAction{
				Kind: action.Kind, Scope: action.Scope, Destination: action.Destination,
				ContentPath: action.ContentPath, BeforePathMode: action.BeforePathMode,
				BeforePathExisted: action.BeforePathExisted, BeforeParentExisted: action.BeforeParentExisted,
				ExpectedAfter: action.ExpectedAfter.Clone(), AggregateContract: action.AggregateContract,
			})
		case recovery.ActionKindError:
			return nil, fmt.Errorf("recovery plan contains error action: %s", action.Reason)
		default:
			return nil, fmt.Errorf("unsupported recovery action %q for %q", action.Kind, action.Destination)
		}
	}
	return orderRecoveryHostActions(hostActions), nil
}
