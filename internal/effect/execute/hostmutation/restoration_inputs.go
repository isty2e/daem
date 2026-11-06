package hostmutation

import (
	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func (action RestorationAction) clone() RestorationAction {
	cloned := action
	cloned.ExpectedAfter = action.ExpectedAfter.Clone()
	if action.BeforePathMode != nil {
		mode := *action.BeforePathMode
		cloned.BeforePathMode = &mode
	}
	if action.AggregateContract != nil {
		contract := action.AggregateContract.Clone()
		cloned.AggregateContract = &contract
	}
	return cloned
}

func RestorationBackupRequests(actions []RestorationAction) []BackupRequest {
	requests := make([]BackupRequest, 0, len(actions))
	for index, action := range actions {
		if action.Kind == recovery.ActionKindRestoreWrite {
			requests = append(requests, action.backupRequest(index))
		}
	}
	return requests
}

func (action RestorationAction) backupRequest(index int) BackupRequest {
	return BackupRequest{
		Index: index, BackupPath: action.BackupPath, BackupKind: action.BackupKind,
		BackupHash: action.BackupHash, BackupWork: action.BackupWork,
	}
}
