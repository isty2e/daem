package rootedpath

import (
	"path/filepath"
	"strings"
	"sync"
)

// EntryAuthority owns one exact entry binding. It borrows the selected root
// for descendants and owns an independently captured root for external entries.
type EntryAuthority struct {
	mu          sync.Mutex
	root        *CapturedRoot
	destination Destination
	ownsRoot    bool
	closed      bool
}

// BindSelectedEntryAuthority validates the selected root and binds one exact
// entry. Entries outside the selected root receive independent physical-root
// authority without weakening selected-root validation.
func BindSelectedEntryAuthority(
	selected *CapturedRoot,
	selectedRoot string,
	selectedPath string,
) (*EntryAuthority, error) {
	if selected == nil {
		return nil, newFailure(
			FailureRootUnavailable,
			selectedRoot,
			"captured selected root is required",
			nil,
		)
	}
	if err := selected.ValidateSelection(selectedRoot); err != nil {
		return nil, err
	}
	child, same, err := selectedEntryRelation(selectedRoot, selectedPath)
	if err != nil {
		return nil, err
	}
	if same {
		return nil, newFailure(
			FailureInvalidDestination,
			selectedPath,
			"selected entry must not be the selected root",
			nil,
		)
	}
	if child {
		destination, err := selected.bindSelectedEntry(selectedRoot, selectedPath)
		if err != nil {
			return nil, err
		}
		return &EntryAuthority{
			root:        selected,
			destination: destination,
		}, nil
	}
	root, destination, err := CaptureDestination(selectedPath)
	if err != nil {
		return nil, err
	}
	return &EntryAuthority{
		root:        root,
		destination: destination,
		ownsRoot:    true,
	}, nil
}

// Acquire issues one commit capability for the bound entry.
func (authority *EntryAuthority) Acquire() (CommitCapability, error) {
	if authority == nil {
		return nil, newFailure(
			FailureRootUnavailable,
			"",
			"entry authority is required",
			nil,
		)
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed || authority.root == nil {
		return nil, newFailure(
			FailureRootUnavailable,
			"",
			"entry authority is closed",
			nil,
		)
	}
	return authority.root.Acquire(authority.destination)
}

// Close releases an independently captured root. Borrowed selected roots
// remain owned by their caller.
func (authority *EntryAuthority) Close() error {
	if authority == nil {
		return nil
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed {
		return nil
	}
	authority.closed = true
	if !authority.ownsRoot || authority.root == nil {
		authority.root = nil
		return nil
	}
	err := authority.root.Close()
	authority.root = nil
	return err
}

func selectedEntryRelation(
	selectedRoot string,
	selectedPath string,
) (child bool, same bool, err error) {
	if strings.TrimSpace(selectedRoot) == "" {
		return false, false, newFailure(
			FailureInvalidRoot,
			selectedRoot,
			"selected root is required",
			nil,
		)
	}
	if strings.TrimSpace(selectedPath) == "" {
		return false, false, newFailure(
			FailureInvalidDestination,
			selectedPath,
			"selected entry is required",
			nil,
		)
	}
	rootPath, err := filepath.Abs(filepath.Clean(selectedRoot))
	if err != nil {
		return false, false, newFailure(
			FailureInvalidRoot,
			selectedRoot,
			"resolve selected root",
			err,
		)
	}
	entryPath, err := filepath.Abs(filepath.Clean(selectedPath))
	if err != nil {
		return false, false, newFailure(
			FailureInvalidDestination,
			selectedPath,
			"resolve selected entry",
			err,
		)
	}
	relative, err := filepath.Rel(rootPath, entryPath)
	if err != nil {
		return false, false, newFailure(
			FailureInvalidDestination,
			selectedPath,
			"compare selected entry with selected root",
			err,
		)
	}
	if relative == "." {
		return false, true, nil
	}
	return !filepath.IsAbs(relative) &&
		relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)), false, nil
}
