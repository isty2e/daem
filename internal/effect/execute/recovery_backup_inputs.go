package execute

import (
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func recoveryBackupRequests(actions []recoveryHostAction) []hostmutation.BackupRequest {
	requests := make([]hostmutation.BackupRequest, 0, len(actions))
	for index, action := range actions {
		if action.Kind == recovery.ActionKindRestoreWrite {
			requests = append(requests, action.backupRequest(index))
		}
	}
	return requests
}

func (action recoveryHostAction) backupRequest(index int) hostmutation.BackupRequest {
	return hostmutation.BackupRequest{
		Index: index, BackupPath: action.BackupPath, BackupKind: action.BackupKind,
		BackupHash: action.BackupHash, BackupWork: action.BackupWork,
	}
}
