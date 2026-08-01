package journal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func (entry *recoveryEntry) UnmarshalJSON(content []byte) error {
	if entry == nil {
		return fmt.Errorf("recovery entry destination is nil")
	}
	if err := requireRecoveryJSONFields(
		content,
		"recovery entry",
		"subject",
		"scope",
		"path",
		"before",
		"expected_after",
		"state_before",
		"state_expected_after",
	); err != nil {
		return err
	}
	type wire recoveryEntry
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*entry = recoveryEntry(decoded)
	return nil
}

// recoveryBeforePathDTO is the exact journal-v9 representation of physical
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

func (persisted *recoveryBeforePathDTO) UnmarshalJSON(content []byte) error {
	if persisted == nil {
		return fmt.Errorf("recovery before-path destination is nil")
	}
	if err := requireRecoveryJSONFields(
		content,
		"recovery before path",
		"existed",
	); err != nil {
		return err
	}
	type wire recoveryBeforePathDTO
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*persisted = recoveryBeforePathDTO(decoded)
	return nil
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

// recoveryExpectedPathDTO is the exact journal-v9 representation of physical
// expected-after facts.
type recoveryExpectedPathDTO struct {
	Existed     bool                     `json:"existed"`
	PathExisted bool                     `json:"path_existed,omitempty"`
	PathMode    *recovery.PermissionMode `json:"path_mode,omitempty"`
	Kind        string                   `json:"kind,omitempty"`
	ContentHash string                   `json:"content_hash,omitempty"`
	LinkTarget  string                   `json:"link_target,omitempty"`
}

func (persisted *recoveryExpectedPathDTO) UnmarshalJSON(content []byte) error {
	if persisted == nil {
		return fmt.Errorf("recovery expected-path destination is nil")
	}
	if err := requireRecoveryJSONFields(
		content,
		"recovery expected path",
		"existed",
	); err != nil {
		return err
	}
	type wire recoveryExpectedPathDTO
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*persisted = recoveryExpectedPathDTO(decoded)
	return nil
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

func (membership *recoveryManagedMembership) UnmarshalJSON(content []byte) error {
	if membership == nil {
		return fmt.Errorf("recovery managed-membership destination is nil")
	}
	if err := requireRecoveryJSONFields(
		content,
		"recovery managed membership",
		"managed",
	); err != nil {
		return err
	}
	type wire recoveryManagedMembership
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*membership = recoveryManagedMembership(decoded)
	return nil
}

func requireRecoveryJSONFields(
	content []byte,
	context string,
	names ...string,
) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return fmt.Errorf("%s must be a JSON object: %w", context, err)
	}
	if fields == nil {
		return fmt.Errorf("%s must be a JSON object", context)
	}
	for _, name := range names {
		value, present := fields[name]
		if !present {
			return fmt.Errorf("%s field %q is required", context, name)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%s field %q must not be null", context, name)
		}
	}
	return nil
}

func decodeRecoveryJSONStrict[T any](content []byte, destination *T) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("recovery JSON value contains trailing data")
		}
		return err
	}
	return nil
}
