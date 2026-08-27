package rootedpath

import (
	"path/filepath"
	"strings"
	"sync"
)

// EntryAuthority owns one exact entry binding. It borrows the selected root
// for descendants and owns an independently captured root for external entries.
type EntryAuthority struct {
	mu                   sync.Mutex
	root                 *CapturedRoot
	destination          Destination
	maximumPhysicalDepth int
	budget               PhysicalTraversalBudget
	ownsRoot             bool
	closed               bool
}

// BindSelectedEntryAuthority validates the selected root and binds one exact
// entry. Entries outside the selected root receive independent physical-root
// authority without weakening selected-root validation.
func BindSelectedEntryAuthority(
	selected *CapturedRoot,
	selectedRoot string,
	selectedPath string,
) (*EntryAuthority, error) {
	return bindSelectedEntryAuthority(selected, selectedRoot, selectedPath, 0, nil, false)
}

// BindSelectedEntryAuthorityBounded binds one exact entry while charging
// selected-root validation and any external path capture to one operation
// budget. Every later Acquire uses that supplied budget.
func BindSelectedEntryAuthorityBounded(
	selected *CapturedRoot,
	selectedRoot string,
	selectedPath string,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) (*EntryAuthority, error) {
	if maximumPhysicalDepth <= 0 {
		return nil, newFailure(
			FailureInvalidRoot,
			selectedRoot,
			"maximum physical depth must be positive",
			nil,
		)
	}
	if budget == nil {
		return nil, newFailure(
			FailureRootUnavailable,
			selectedRoot,
			"entry authority traversal budget is required",
			nil,
		)
	}
	return bindSelectedEntryAuthority(
		selected,
		selectedRoot,
		selectedPath,
		maximumPhysicalDepth,
		budget,
		false,
	)
}

// BindCanonicalSelectedEntryAuthorityBounded validates the selected root and
// binds the entry through independent native canonical path authority. It is
// for already-normalized control paths whose authority remains descriptor-bound.
func BindCanonicalSelectedEntryAuthorityBounded(
	selected *CapturedRoot,
	selectedRoot string,
	selectedPath string,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) (*EntryAuthority, error) {
	if maximumPhysicalDepth <= 0 {
		return nil, newFailure(
			FailureInvalidRoot,
			selectedRoot,
			"maximum physical depth must be positive",
			nil,
		)
	}
	if budget == nil {
		return nil, newFailure(
			FailureRootUnavailable,
			selectedRoot,
			"entry authority traversal budget is required",
			nil,
		)
	}
	return bindSelectedEntryAuthority(
		selected,
		selectedRoot,
		selectedPath,
		maximumPhysicalDepth,
		budget,
		true,
	)
}

// BindCapturedEntryAuthorityBounded attaches bounded future access to one
// destination already issued by the same captured root. It performs no path
// traversal; callers must have established and budgeted the root/destination
// pair before using this pure binding step.
func BindCapturedEntryAuthorityBounded(
	root *CapturedRoot,
	destination Destination,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) (*EntryAuthority, error) {
	if root == nil {
		return nil, newFailure(FailureRootUnavailable, "", "captured root is required", nil)
	}
	if maximumPhysicalDepth <= 0 {
		return nil, newFailure(
			FailureInvalidRoot,
			"",
			"maximum physical depth must be positive",
			nil,
		)
	}
	if budget == nil {
		return nil, newFailure(
			FailureRootUnavailable,
			"",
			"entry authority traversal budget is required",
			nil,
		)
	}
	if err := destination.Validate(); err != nil {
		return nil, err
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.closed {
		return nil, newFailure(
			FailureRootUnavailable,
			root.authority.physicalRoot,
			"captured root is closed",
			nil,
		)
	}
	if !root.authority.Equal(destination.root) {
		return nil, newFailure(
			FailureInvalidDestination,
			destination.relative.value,
			"destination belongs to a different root authority",
			nil,
		)
	}
	return &EntryAuthority{
		root:                 root,
		destination:          destination,
		maximumPhysicalDepth: maximumPhysicalDepth,
		budget:               budget,
	}, nil
}

func bindSelectedEntryAuthority(
	selected *CapturedRoot,
	selectedRoot string,
	selectedPath string,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
	canonicalExternal bool,
) (*EntryAuthority, error) {
	if selected == nil {
		return nil, newFailure(
			FailureRootUnavailable,
			selectedRoot,
			"captured selected root is required",
			nil,
		)
	}
	var validationErr error
	if budget == nil {
		validationErr = selected.ValidateSelection(selectedRoot)
	} else {
		validationErr = selected.ValidateSelectionBounded(
			selectedRoot,
			maximumPhysicalDepth,
			budget,
		)
	}
	if validationErr != nil {
		return nil, validationErr
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
	if child && !canonicalExternal {
		destination, err := selected.bindSelectedEntryAfterValidation(selectedRoot, selectedPath)
		if err != nil {
			return nil, err
		}
		return &EntryAuthority{
			root:                 selected,
			destination:          destination,
			maximumPhysicalDepth: maximumPhysicalDepth,
			budget:               budget,
		}, nil
	}
	var root *CapturedRoot
	var destination Destination
	if budget == nil {
		root, destination, err = CaptureDestination(selectedPath)
	} else if canonicalExternal {
		root, destination, err = CaptureCanonicalDestinationBounded(
			selectedPath,
			maximumPhysicalDepth,
			budget,
		)
	} else {
		root, destination, err = CaptureDestinationBounded(
			selectedPath,
			maximumPhysicalDepth,
			budget,
		)
	}
	if err != nil {
		return nil, err
	}
	return &EntryAuthority{
		root:                 root,
		destination:          destination,
		maximumPhysicalDepth: maximumPhysicalDepth,
		budget:               budget,
		ownsRoot:             true,
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
	if authority.budget != nil {
		return authority.root.AcquireBounded(
			authority.destination,
			authority.maximumPhysicalDepth,
			authority.budget,
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
