// Package recovery owns the wire-neutral interrupted-operation authority and
// pure recovery classification algebra derived from recovery journals.
package recovery

import "io/fs"

const (
	// PathKindFile records a regular file backup payload.
	PathKindFile = "file"
	// PathKindDirectory records a directory backup payload.
	PathKindDirectory = "directory"
	// PathKindSymlink records a symbolic link target.
	PathKindSymlink = "symlink"
)

// PermissionMode is a regular file's semantic permission-bit value. A pointer
// distinguishes an explicit mode 0000 from missing evidence.
type PermissionMode uint32

// NewPermissionMode captures only permission bits from an observed file mode.
func NewPermissionMode(mode fs.FileMode) *PermissionMode {
	value := PermissionMode(mode.Perm())
	return &value
}

// FileMode returns the standard-library representation of the permission bits.
func (mode PermissionMode) FileMode() fs.FileMode {
	return fs.FileMode(mode)
}

// BeforePathState records the selected value and physical path facts needed to
// restore the previous host state. PathExisted and PathMode describe the
// containing regular file for content-path projections; whole-path state
// derives physical existence from Existed.
type BeforePathState struct {
	Existed       bool
	PathExisted   bool
	ParentExisted bool
	PathMode      *PermissionMode
	Kind          string
	ContentHash   string
	BackupPath    string
	LinkTarget    string
}

// ExpectedPathState records the selected value and physical path facts restore
// must see before replaying.
type ExpectedPathState struct {
	Existed     bool
	PathExisted bool
	PathMode    *PermissionMode
	Kind        string
	ContentHash string
	LinkTarget  string
}

// Clone returns an independent expected-path value.
func (state ExpectedPathState) Clone() ExpectedPathState {
	clone := state
	if state.PathMode != nil {
		mode := *state.PathMode
		clone.PathMode = &mode
	}
	return clone
}

// Equal compares every physical and selected-value expectation.
func (state ExpectedPathState) Equal(other ExpectedPathState) bool {
	if state.Existed != other.Existed ||
		state.PathExisted != other.PathExisted ||
		state.Kind != other.Kind ||
		state.ContentHash != other.ContentHash ||
		state.LinkTarget != other.LinkTarget {
		return false
	}
	if state.PathMode == nil || other.PathMode == nil {
		return state.PathMode == nil && other.PathMode == nil
	}
	return *state.PathMode == *other.PathMode
}

func clonePermissionMode(mode *PermissionMode) *PermissionMode {
	if mode == nil {
		return nil
	}
	clone := *mode
	return &clone
}
