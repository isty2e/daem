package journal

import "github.com/isty2e/daem/internal/effect/journal/recovery"

// recoveryBeforePathDTO is the exact journal-v7 representation of physical
// before-state facts. It remains private so wire syntax cannot become recovery
// policy.
type recoveryBeforePathDTO struct {
	Existed       bool                     `json:"existed"`
	PathExisted   bool                     `json:"path_existed,omitempty"`
	ParentExisted bool                     `json:"parent_existed,omitempty"`
	PathMode      *recovery.PermissionMode `json:"path_mode,omitempty"`
	Kind          string                   `json:"kind,omitempty"`
	ContentHash   string                   `json:"content_hash,omitempty"`
	BackupPath    string                   `json:"backup_path,omitempty"`
	LinkTarget    string                   `json:"link_target,omitempty"`
}

func persistedBeforePathState(state recovery.BeforePathState) recoveryBeforePathDTO {
	return recoveryBeforePathDTO{
		Existed:       state.Existed,
		PathExisted:   state.PathExisted,
		ParentExisted: state.ParentExisted,
		PathMode:      clonePermissionMode(state.PathMode),
		Kind:          state.Kind,
		ContentHash:   state.ContentHash,
		BackupPath:    state.BackupPath,
		LinkTarget:    state.LinkTarget,
	}
}

func (persisted recoveryBeforePathDTO) canonical() recovery.BeforePathState {
	return recovery.BeforePathState{
		Existed:       persisted.Existed,
		PathExisted:   persisted.PathExisted,
		ParentExisted: persisted.ParentExisted,
		PathMode:      clonePermissionMode(persisted.PathMode),
		Kind:          persisted.Kind,
		ContentHash:   persisted.ContentHash,
		BackupPath:    persisted.BackupPath,
		LinkTarget:    persisted.LinkTarget,
	}
}

// recoveryExpectedPathDTO is the exact journal-v7 representation of physical
// expected-after facts.
type recoveryExpectedPathDTO struct {
	Existed     bool                     `json:"existed"`
	PathExisted bool                     `json:"path_existed,omitempty"`
	PathMode    *recovery.PermissionMode `json:"path_mode,omitempty"`
	Kind        string                   `json:"kind,omitempty"`
	ContentHash string                   `json:"content_hash,omitempty"`
	LinkTarget  string                   `json:"link_target,omitempty"`
}

func persistedExpectedPathState(state recovery.ExpectedPathState) recoveryExpectedPathDTO {
	return recoveryExpectedPathDTO{
		Existed:     state.Existed,
		PathExisted: state.PathExisted,
		PathMode:    clonePermissionMode(state.PathMode),
		Kind:        state.Kind,
		ContentHash: state.ContentHash,
		LinkTarget:  state.LinkTarget,
	}
}

func (persisted recoveryExpectedPathDTO) canonical() recovery.ExpectedPathState {
	return recovery.ExpectedPathState{
		Existed:     persisted.Existed,
		PathExisted: persisted.PathExisted,
		PathMode:    clonePermissionMode(persisted.PathMode),
		Kind:        persisted.Kind,
		ContentHash: persisted.ContentHash,
		LinkTarget:  persisted.LinkTarget,
	}
}

func clonePermissionMode(mode *recovery.PermissionMode) *recovery.PermissionMode {
	if mode == nil {
		return nil
	}
	clone := *mode
	return &clone
}
