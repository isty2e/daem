package recovery

import (
	"fmt"
	pathpkg "path"
	"strings"
)

// ValidateBefore rejects malformed pre-mutation physical path evidence.
func ValidateBefore(state BeforePathState, contentPath string) error {
	const context = "before"
	if !state.Existed {
		if state.Kind != "" || state.ContentHash != "" || state.BackupPath != "" || state.LinkTarget != "" {
			return fmt.Errorf("%s: absent paths must not record file metadata", context)
		}
		if contentPath == "" && (state.PathExisted || state.ParentExisted || state.PathMode != nil) {
			return fmt.Errorf("%s: full-path absence must not record aggregate path metadata", context)
		}
		if !state.PathExisted && state.ParentExisted {
			return fmt.Errorf("%s.parent_existed: requires path_existed", context)
		}
		if !state.PathExisted && state.PathMode != nil {
			return fmt.Errorf("%s.path_mode: requires path_existed", context)
		}
		if state.PathExisted {
			if state.PathMode == nil {
				return fmt.Errorf("%s.path_mode: required when physical path exists", context)
			}
			return validatePermissionMode(*state.PathMode, context+".path_mode")
		}
		return nil
	}
	if state.PathExisted || state.ParentExisted {
		if contentPath == "" {
			return fmt.Errorf("%s: full-path state must not record aggregate path metadata", context)
		}
		if !state.PathExisted {
			return fmt.Errorf("%s.parent_existed: requires path_existed", context)
		}
	}
	if contentPath != "" && !state.PathExisted {
		return fmt.Errorf("%s.path_existed: required for existing content path", context)
	}

	switch state.Kind {
	case PathKindFile:
		if strings.TrimSpace(state.ContentHash) == "" {
			return fmt.Errorf("%s.content_hash: required for existing %s", context, state.Kind)
		}
		if !isSafeRelativePath(state.BackupPath) {
			return fmt.Errorf("%s.backup_path: must be a safe relative backup path", context)
		}
		if state.LinkTarget != "" {
			return fmt.Errorf("%s.link_target: must be empty for %s", context, state.Kind)
		}
		if state.PathMode == nil {
			return fmt.Errorf("%s.path_mode: required for existing file", context)
		}
		return validatePermissionMode(*state.PathMode, context+".path_mode")
	case PathKindDirectory:
		if contentPath != "" {
			return fmt.Errorf("%s.kind: content paths require a regular file", context)
		}
		if strings.TrimSpace(state.ContentHash) == "" {
			return fmt.Errorf("%s.content_hash: required for existing %s", context, state.Kind)
		}
		if !isSafeRelativePath(state.BackupPath) {
			return fmt.Errorf("%s.backup_path: must be a safe relative backup path", context)
		}
		if state.LinkTarget != "" {
			return fmt.Errorf("%s.link_target: must be empty for %s", context, state.Kind)
		}
		if state.PathMode != nil {
			return fmt.Errorf("%s.path_mode: must be empty for directory", context)
		}
	case PathKindSymlink:
		if contentPath != "" {
			return fmt.Errorf("%s.kind: content paths require a regular file", context)
		}
		if strings.TrimSpace(state.LinkTarget) == "" {
			return fmt.Errorf("%s.link_target: required for existing symlink", context)
		}
		if state.ContentHash != "" || state.BackupPath != "" || state.PathMode != nil {
			return fmt.Errorf("%s: symlinks must not record content_hash, backup_path, or path_mode", context)
		}
	default:
		return fmt.Errorf("%s.kind: unknown path kind %q", context, state.Kind)
	}
	return nil
}

// ValidateExpected rejects malformed post-mutation physical path evidence.
func ValidateExpected(state ExpectedPathState, contentPath string) error {
	const context = "expected_after"
	if !state.Existed {
		if state.Kind != "" || state.ContentHash != "" || state.LinkTarget != "" {
			return fmt.Errorf("%s: absent paths must not record file metadata", context)
		}
		if contentPath == "" && (state.PathExisted || state.PathMode != nil) {
			return fmt.Errorf("%s: full-path absence must not record aggregate path metadata", context)
		}
		if !state.PathExisted && state.PathMode != nil {
			return fmt.Errorf("%s.path_mode: requires path_existed", context)
		}
		if state.PathExisted {
			if state.PathMode == nil {
				return fmt.Errorf("%s.path_mode: required when physical path exists", context)
			}
			return validatePermissionMode(*state.PathMode, context+".path_mode")
		}
		return nil
	}
	if contentPath != "" && !state.PathExisted {
		return fmt.Errorf("%s.path_existed: required for existing content path", context)
	}
	if contentPath == "" && state.PathExisted {
		return fmt.Errorf("%s.path_existed: must be empty for full path", context)
	}

	switch state.Kind {
	case PathKindFile:
		if strings.TrimSpace(state.ContentHash) == "" {
			return fmt.Errorf("%s.content_hash: required for expected %s", context, state.Kind)
		}
		if state.LinkTarget != "" {
			return fmt.Errorf("%s.link_target: must be empty for %s", context, state.Kind)
		}
		if state.PathMode == nil {
			return fmt.Errorf("%s.path_mode: required for expected file", context)
		}
		return validatePermissionMode(*state.PathMode, context+".path_mode")
	case PathKindDirectory:
		if contentPath != "" {
			return fmt.Errorf("%s.kind: content paths require a regular file", context)
		}
		if strings.TrimSpace(state.ContentHash) == "" {
			return fmt.Errorf("%s.content_hash: required for expected %s", context, state.Kind)
		}
		if state.LinkTarget != "" {
			return fmt.Errorf("%s.link_target: must be empty for %s", context, state.Kind)
		}
		if state.PathMode != nil {
			return fmt.Errorf("%s.path_mode: must be empty for directory", context)
		}
	case PathKindSymlink:
		if contentPath != "" {
			return fmt.Errorf("%s.kind: content paths require a regular file", context)
		}
		if strings.TrimSpace(state.LinkTarget) == "" {
			return fmt.Errorf("%s.link_target: required for expected symlink", context)
		}
		if state.ContentHash != "" || state.PathMode != nil {
			return fmt.Errorf("%s: symlinks must not record content_hash or path_mode", context)
		}
	default:
		return fmt.Errorf("%s.kind: unknown path kind %q", context, state.Kind)
	}
	return nil
}

func validatePermissionMode(mode PermissionMode, context string) error {
	if uint32(mode)&^uint32(0o777) != 0 {
		return fmt.Errorf("%s: must contain only permission bits", context)
	}
	return nil
}

func isSafeRelativePath(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" ||
		trimmed != value ||
		strings.Contains(trimmed, "\\") ||
		strings.HasPrefix(trimmed, "~") ||
		pathpkg.IsAbs(trimmed) {
		return false
	}
	cleaned := pathpkg.Clean(trimmed)
	return cleaned == trimmed &&
		cleaned != "." &&
		cleaned != ".." &&
		!strings.HasPrefix(cleaned, "../")
}
