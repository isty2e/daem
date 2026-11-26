package hostmutation

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/topology"
)

func RestorationActionsFor(actions []recovery.Action, before durable.Snapshot) ([]RestorationAction, error) {
	trees := make(map[topology.SubjectID]durable.ManagedPathState)
	for _, path := range before.ManagedPaths() {
		if path.TreeMembers().Known() {
			trees[path.Subject()] = path
		}
	}
	hostActions := make([]RestorationAction, 0, len(actions))
	for _, action := range actions {
		switch action.Kind {
		case recovery.ActionKindNoOp:
			continue
		case recovery.ActionKindRestoreWrite:
			restore := RestorationAction{
				Kind: action.Kind, Scope: action.Scope, Destination: action.Destination,
				ContentPath: action.ContentPath, BackupPath: action.BackupPath,
				BackupHash: action.BackupHash, BackupKind: action.BackupKind, BackupWork: action.BackupWork,
				TreeModes:      action.TreeModes,
				BeforePathMode: action.BeforePathMode, BeforePathExisted: action.BeforePathExisted,
				BeforeParentExisted: action.BeforeParentExisted, ExpectedAfter: action.ExpectedRestorationInput(),
				AggregateContract: action.AggregateContract,
			}
			id, _ := action.SubjectID()
			if tree, ok := trees[id]; ok && tree.Scope() == action.Scope && tree.Destination().String() == action.Destination {
				restore.survivingMembers = tree.TreeMembers()
			}
			hostActions = append(hostActions, restore)
		case recovery.ActionKindRestoreDelete:
			hostActions = append(hostActions, RestorationAction{
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
	return OrderRestorationActions(hostActions), nil
}
