package rootedpath

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// CapturedRoot retains the native witness for one selected physical directory
// root. It issues destination-bound capabilities but owns no host payload or
// workflow sequencing.
type CapturedRoot struct {
	mu            sync.Mutex
	authority     Authority
	platform      capturedRootPlatform
	selectionMode rootSelectionMode
	closed        bool
}

type rootSelectionMode uint8

const (
	rootSelectionInvalid rootSelectionMode = iota
	rootSelectionResolveAlias
	rootSelectionNoFollow
)

// CaptureRoot resolves a selected root alias once and retains a native witness
// for the resulting physical directory.
func CaptureRoot(selectedRoot string) (*CapturedRoot, error) {
	return captureRoot(selectedRoot, rootSelectionResolveAlias)
}

// CaptureRootNoFollow retains a native witness only when every selected path
// component is a directory rather than a symbolic link.
func CaptureRootNoFollow(selectedRoot string) (*CapturedRoot, error) {
	return captureRoot(selectedRoot, rootSelectionNoFollow)
}

// CaptureRootNoFollowBounded retains one physical root while charging every
// opened component to the supplied operation budget.
func CaptureRootNoFollowBounded(
	selectedRoot string,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) (*CapturedRoot, error) {
	traversal, err := newPhysicalTraversal(maximumPhysicalDepth, budget)
	if err != nil {
		return nil, err
	}
	return captureRootWithTraversal(selectedRoot, rootSelectionNoFollow, traversal)
}

// CaptureRootBounded resolves one selected root while charging every native
// component visit to the supplied operation budget.
func CaptureRootBounded(
	selectedRoot string,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) (*CapturedRoot, error) {
	traversal, err := newPhysicalTraversal(maximumPhysicalDepth, budget)
	if err != nil {
		return nil, err
	}
	return captureRootWithTraversal(selectedRoot, rootSelectionResolveAlias, traversal)
}

func captureRootBounded(
	selectedRoot string,
	selectionMode rootSelectionMode,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) (*CapturedRoot, error) {
	traversal, err := newPhysicalTraversal(maximumPhysicalDepth, budget)
	if err != nil {
		return nil, err
	}
	return captureRootWithTraversal(selectedRoot, selectionMode, traversal)
}

func captureRoot(selectedRoot string, selectionMode rootSelectionMode) (*CapturedRoot, error) {
	return captureRootWithTraversal(selectedRoot, selectionMode, nil)
}

func captureRootWithTraversal(
	selectedRoot string,
	selectionMode rootSelectionMode,
	traversal *physicalTraversal,
) (*CapturedRoot, error) {
	physicalRoot, platform, object, mount, err := captureRootPlatform(
		selectedRoot,
		selectionMode,
		traversal,
	)
	if err != nil {
		return nil, err
	}
	return newCapturedRoot(physicalRoot, platform, object, mount, selectionMode)
}

func newCapturedRoot(
	physicalRoot string,
	platform capturedRootPlatform,
	object identityToken,
	mount mountIdentities,
	selectionMode rootSelectionMode,
) (*CapturedRoot, error) {
	authority, err := newCapturedAuthority(physicalRoot, object, mount)
	if err != nil {
		_ = closeCapturedRootPlatform(&platform)
		return nil, err
	}
	return &CapturedRoot{
		authority:     authority,
		platform:      platform,
		selectionMode: selectionMode,
	}, nil
}

// PhysicalTraversalBudget charges actual component visits while a selected
// path is resolved and its physical root is opened. The owner of an operation
// budget supplies the policy; rootedpath owns where traversal work occurs.
type PhysicalTraversalBudget interface {
	AdmitPathComponents(count int) error
}

type physicalWorkBudget interface {
	AdmitPhysicalWork(pathComponents int, entries int, bytes int64) error
}

// ChildrenExistNoFollow observes one immediate-child pair through one fresh
// retained-root validation without interpreting names as paths or following
// symlinks.
func (root *CapturedRoot) ChildrenExistNoFollow(
	ctx context.Context,
	names [2]string,
	budget PhysicalTraversalBudget,
) ([2]bool, error) {
	if root == nil {
		return [2]bool{}, newFailure(FailureRootUnavailable, "", "captured root is required", nil)
	}
	if ctx == nil {
		return [2]bool{}, fmt.Errorf("root child observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return [2]bool{}, err
	}
	if budget == nil {
		return [2]bool{}, fmt.Errorf("root child observation budget is required")
	}
	for _, name := range names {
		if name == "" || name == "." || name == ".." || filepath.Clean(name) != name ||
			filepath.IsAbs(name) || filepath.Base(name) != name ||
			strings.IndexFunc(name, isForbiddenPathRune) >= 0 {
			return [2]bool{}, newFailure(FailureInvalidDestination, name, "immediate child name is invalid", nil)
		}
		if err := validatePlatformComponent(name); err != nil {
			return [2]bool{}, err
		}
	}
	if names[0] == names[1] {
		return [2]bool{}, newFailure(FailureInvalidDestination, names[0], "immediate child name is duplicated", nil)
	}

	root.mu.Lock()
	defer root.mu.Unlock()
	if root.closed {
		return [2]bool{}, newFailure(FailureRootUnavailable, root.authority.physicalRoot, "captured root is closed", nil)
	}
	for _, name := range names {
		if err := validatePlatformRelativeForRoot(&root.platform, name); err != nil {
			return [2]bool{}, err
		}
	}
	validationVisits, err := capturedRootValidationPathComponents(&root.platform)
	if err != nil {
		return [2]bool{}, err
	}
	if err := budget.AdmitPathComponents(validationVisits); err != nil {
		return [2]bool{}, fmt.Errorf("admit retained-root validation: %w", err)
	}
	if err := budget.AdmitPathComponents(len(names)); err != nil {
		return [2]bool{}, fmt.Errorf("admit retained-root child probes: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return [2]bool{}, err
	}
	if err := validateCapturedRootPlatform(&root.platform); err != nil {
		return [2]bool{}, err
	}
	var result [2]bool
	for index, name := range names {
		if err := ctx.Err(); err != nil {
			return [2]bool{}, err
		}
		exists, err := capturedRootChildExistsNoFollow(&root.platform, name)
		if err != nil {
			return [2]bool{}, err
		}
		result[index] = exists
	}
	return result, nil
}

// CaptureDestinationBounded binds one destination while bounding both alias
// resolution and physical-root opening. The maximum applies to the resolved
// physical destination, not merely the selected lexical spelling.
func CaptureDestinationBounded(
	path string,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) (*CapturedRoot, Destination, error) {
	traversal, err := newPhysicalTraversal(maximumPhysicalDepth, budget)
	if err != nil {
		return nil, Destination{}, err
	}
	return captureDestinationBounded(path, traversal)
}

// CaptureDestination binds one absolute destination to the nearest existing
// directory ancestor and retains that ancestor's native witness. Missing
// descendants remain root-relative names, so later mutation never resolves
// the selected path through ambient ancestors again.
func CaptureDestination(path string) (*CapturedRoot, Destination, error) {
	return captureDestinationWithSelection(path, rootSelectionResolveAlias)
}

// CaptureDestinationNoFollow binds one absolute destination the same way as
// CaptureDestination but refuses alias components in the retained physical
// root. A destination whose existing ancestor chain contains a symbolic link,
// junction, or other reparse component fails closed before any effect rather
// than adopting the alias referent as the lexical namespace.
func CaptureDestinationNoFollow(path string) (*CapturedRoot, Destination, error) {
	return captureDestinationWithSelection(path, rootSelectionNoFollow)
}

func captureDestinationWithSelection(
	path string,
	selectionMode rootSelectionMode,
) (*CapturedRoot, Destination, error) {
	absolute, err := canonicalDestinationPath(path)
	if err != nil {
		return nil, Destination{}, err
	}

	ancestor := filepath.Dir(absolute)
	for {
		info, inspectErr := os.Stat(ancestor)
		if inspectErr == nil {
			if !info.IsDir() {
				return nil, Destination{}, newFailure(FailureRootUnavailable, ancestor, "destination ancestor is not a directory", nil)
			}
			if filepath.Dir(ancestor) == ancestor {
				return nil, Destination{}, newFailure(FailureUnsupportedPlatform, absolute, "destination has no capturable directory below the filesystem root", nil)
			}
			root, captureErr := captureRoot(ancestor, selectionMode)
			if captureErr != nil {
				return nil, Destination{}, captureErr
			}
			authority, authorityErr := root.Authority()
			if authorityErr != nil {
				_ = root.Close()
				return nil, Destination{}, authorityErr
			}
			relativeValue, relativeErr := filepath.Rel(ancestor, absolute)
			if relativeErr != nil {
				_ = root.Close()
				return nil, Destination{}, newFailure(FailureInvalidDestination, absolute, "derive root-relative destination", relativeErr)
			}
			relative, relativeErr := NewRelativeDestination(filepath.ToSlash(relativeValue))
			if relativeErr != nil {
				_ = root.Close()
				return nil, Destination{}, relativeErr
			}
			destination, bindErr := authority.Bind(relative)
			if bindErr != nil {
				_ = root.Close()
				return nil, Destination{}, bindErr
			}
			return root, destination, nil
		}
		if !os.IsNotExist(inspectErr) {
			return nil, Destination{}, newFailure(FailureRootUnavailable, ancestor, "inspect destination ancestor", inspectErr)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return nil, Destination{}, newFailure(FailureUnsupportedPlatform, absolute, "destination has no capturable directory ancestor", inspectErr)
		}
		ancestor = parent
	}
}

// Authority returns the canonical identity facts retained by this witness.
func (root *CapturedRoot) Authority() (Authority, error) {
	if root == nil {
		return Authority{}, newFailure(FailureRootUnavailable, "", "captured root is required", nil)
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.closed {
		return Authority{}, newFailure(FailureRootUnavailable, root.authority.physicalRoot, "captured root is closed", nil)
	}
	if err := validateCapturedRootPlatform(&root.platform); err != nil {
		return Authority{}, err
	}
	return root.authority, nil
}

// AuthorityBounded returns retained authority after charging the complete
// validation chain to the caller's operation budget.
func (root *CapturedRoot) AuthorityBounded(
	budget PhysicalTraversalBudget,
) (Authority, error) {
	if root == nil {
		return Authority{}, newFailure(FailureRootUnavailable, "", "captured root is required", nil)
	}
	if budget == nil {
		return Authority{}, fmt.Errorf("captured root authority budget is required")
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.closed {
		return Authority{}, newFailure(FailureRootUnavailable, root.authority.physicalRoot, "captured root is closed", nil)
	}
	visits, err := capturedRootValidationPathComponents(&root.platform)
	if err != nil {
		return Authority{}, err
	}
	if err := budget.AdmitPathComponents(visits); err != nil {
		return Authority{}, fmt.Errorf("admit captured root validation: %w", err)
	}
	if err := validateCapturedRootPlatform(&root.platform); err != nil {
		return Authority{}, err
	}
	return root.authority, nil
}

// ValidateSelection verifies that selectedRoot still resolves to the physical
// root incarnation retained by this witness.
func (root *CapturedRoot) ValidateSelection(selectedRoot string) error {
	expected, err := root.Authority()
	if err != nil {
		return err
	}
	return validateSelectionAgainstAuthority(expected, selectedRoot, root.selectionMode, 0, nil)
}

// ValidateSelectionBounded verifies one selected root while charging every
// physical traversal to the caller's operation budget.
func (root *CapturedRoot) ValidateSelectionBounded(
	selectedRoot string,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) error {
	if budget == nil {
		return fmt.Errorf("selected root validation budget is required")
	}
	expected, err := root.AuthorityBounded(budget)
	if err != nil {
		return err
	}
	return validateSelectionAgainstAuthority(
		expected,
		selectedRoot,
		root.selectionMode,
		maximumPhysicalDepth,
		budget,
	)
}

func validateSelectionAgainstAuthority(
	expected Authority,
	selectedRoot string,
	selectionMode rootSelectionMode,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) error {
	var (
		current *CapturedRoot
		err     error
	)
	if budget == nil {
		current, err = captureRoot(selectedRoot, selectionMode)
	} else {
		current, err = captureRootBounded(
			selectedRoot,
			selectionMode,
			maximumPhysicalDepth,
			budget,
		)
	}
	if err != nil {
		return err
	}
	defer current.Close()
	var observed Authority
	if budget == nil {
		observed, err = current.Authority()
	} else {
		observed, err = current.AuthorityBounded(budget)
	}
	if err != nil {
		return err
	}
	if expected.physicalRoot != observed.physicalRoot {
		return newFailure(
			FailureRootReplaced,
			selectedRoot,
			"selected root no longer resolves to the captured physical root",
			nil,
		)
	}
	if expected.mount.operation != observed.mount.operation {
		return newFailure(
			FailureMountChanged,
			selectedRoot,
			"selected root mount identity differs from the captured authority",
			nil,
		)
	}
	if expected.object != observed.object {
		return newFailure(
			FailureRootReplaced,
			selectedRoot,
			"selected root object identity differs from the captured authority",
			nil,
		)
	}
	return nil
}

// Acquire binds a fresh native capability to exactly one destination under the
// captured root. The returned capability remains independent of later root
// witness closure.
func (root *CapturedRoot) Acquire(destination Destination) (CommitCapability, error) {
	return root.acquire(destination, 0, nil)
}

// AcquireBounded issues one capability whose root validation and every later
// destination-bound storage operation consume the caller's operation budget.
func (root *CapturedRoot) AcquireBounded(
	destination Destination,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) (CommitCapability, error) {
	if maximumPhysicalDepth <= 0 {
		return nil, fmt.Errorf("commit capability maximum physical depth must be positive")
	}
	if budget == nil {
		return nil, fmt.Errorf("captured root acquisition budget is required")
	}
	return root.acquire(destination, maximumPhysicalDepth, budget)
}

// ReserveDestinationAccess charges the exact path work later consumed by one
// AcquireBounded plus one destination-bound filesystem operation. It performs
// no filesystem I/O and grants no capability.
func (root *CapturedRoot) ReserveDestinationAccess(
	destination Destination,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) error {
	if root == nil {
		return newFailure(FailureRootUnavailable, "", "captured root is required", nil)
	}
	if budget == nil {
		return fmt.Errorf("destination access reservation budget is required")
	}
	if err := destination.Validate(); err != nil {
		return err
	}

	root.mu.Lock()
	defer root.mu.Unlock()
	if root.closed {
		return newFailure(FailureRootUnavailable, root.authority.physicalRoot, "captured root is closed", nil)
	}
	if !root.authority.Equal(destination.root) {
		return newFailure(
			FailureInvalidDestination,
			destination.relative.value,
			"destination belongs to a different root authority",
			nil,
		)
	}
	if err := validatePlatformRelativeForRoot(&root.platform, destination.relative.value); err != nil {
		return err
	}
	visits, err := capturedRootValidationPathComponents(&root.platform)
	if err != nil {
		return err
	}
	if err := budget.AdmitPathComponents(visits * 2); err != nil {
		return fmt.Errorf("reserve captured root capability acquisition: %w", err)
	}
	return ChargeDestinationPath(destination, maximumPhysicalDepth, budget)
}

func (root *CapturedRoot) acquire(
	destination Destination,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) (CommitCapability, error) {
	if root == nil {
		return nil, newFailure(FailureRootUnavailable, "", "captured root is required", nil)
	}
	if err := destination.Validate(); err != nil {
		return nil, err
	}

	root.mu.Lock()
	defer root.mu.Unlock()
	if root.closed {
		return nil, newFailure(FailureRootUnavailable, root.authority.physicalRoot, "captured root is closed", nil)
	}
	if !root.authority.Equal(destination.root) {
		return nil, newFailure(
			FailureInvalidDestination,
			destination.relative.value,
			"destination belongs to a different root authority",
			nil,
		)
	}
	if err := validatePlatformRelativeForRoot(&root.platform, destination.relative.value); err != nil {
		return nil, err
	}
	if budget != nil {
		visits, err := capturedRootValidationPathComponents(&root.platform)
		if err != nil {
			return nil, err
		}
		if err := budget.AdmitPathComponents(visits * 2); err != nil {
			return nil, fmt.Errorf("admit captured root capability acquisition: %w", err)
		}
	}
	if err := validateCapturedRootPlatform(&root.platform); err != nil {
		return nil, err
	}
	platform, err := cloneCapturedRootPlatform(&root.platform)
	if err != nil {
		return nil, newFailure(FailureRootUnavailable, root.authority.physicalRoot, "duplicate captured root witness", err)
	}
	capability := &commitCapability{
		destination:          destination,
		platform:             platform,
		maximumPhysicalDepth: maximumPhysicalDepth,
		budget:               budget,
	}
	if err := validateCapturedRootPlatform(&capability.platform); err != nil {
		_ = capability.Close()
		return nil, err
	}
	return capability, nil
}

// AcquireWorkingDirectory issues a fresh capability for one process launch
// from the captured root. The capability remains independent of later root
// witness closure.
func (root *CapturedRoot) AcquireWorkingDirectory() (WorkingDirectoryCapability, error) {
	return root.acquireWorkingDirectory("", nil)
}

// AcquireWorkingDirectoryBounded issues a root-directory capability after
// charging both retained-root validation passes to the operation budget.
func (root *CapturedRoot) AcquireWorkingDirectoryBounded(
	budget PhysicalTraversalBudget,
) (WorkingDirectoryCapability, error) {
	if budget == nil {
		return nil, fmt.Errorf("working-directory acquisition budget is required")
	}
	return root.acquireWorkingDirectory("", budget)
}

// AcquireSelectedWorkingDirectory issues a process-directory capability that
// also rejects later retargeting of the selected root path.
func (root *CapturedRoot) AcquireSelectedWorkingDirectory(selectedRoot string) (WorkingDirectoryCapability, error) {
	if err := root.ValidateSelection(selectedRoot); err != nil {
		return nil, err
	}
	return root.acquireWorkingDirectory(selectedRoot, nil)
}

func (root *CapturedRoot) acquireWorkingDirectory(
	selectedRoot string,
	budget PhysicalTraversalBudget,
) (WorkingDirectoryCapability, error) {
	if root == nil {
		return nil, newFailure(FailureRootUnavailable, "", "captured root is required", nil)
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.closed {
		return nil, newFailure(FailureRootUnavailable, root.authority.physicalRoot, "captured root is closed", nil)
	}
	if budget != nil {
		visits, err := capturedRootValidationPathComponents(&root.platform)
		if err != nil {
			return nil, err
		}
		if err := budget.AdmitPathComponents(visits * 2); err != nil {
			return nil, fmt.Errorf("admit working-directory capability acquisition: %w", err)
		}
	}
	if err := validateCapturedRootPlatform(&root.platform); err != nil {
		return nil, err
	}
	platform, err := cloneCapturedRootPlatform(&root.platform)
	if err != nil {
		return nil, newFailure(FailureRootUnavailable, root.authority.physicalRoot, "duplicate captured root witness", err)
	}
	capability := &workingDirectoryCapability{
		authority:     root.authority,
		platform:      platform,
		selectedRoot:  selectedRoot,
		selectionMode: root.selectionMode,
	}
	if err := validateCapturedRootPlatform(&capability.platform); err != nil {
		_ = capability.Close()
		return nil, err
	}
	return capability, nil
}

// Close releases the retained root witness. Existing capabilities retain their
// own descriptors and must be closed separately.
func (root *CapturedRoot) Close() error {
	if root == nil {
		return nil
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.closed {
		return nil
	}
	root.closed = true
	return closeCapturedRootPlatform(&root.platform)
}

type commitCapability struct {
	mu                   sync.Mutex
	destination          Destination
	platform             capturedRootPlatform
	maximumPhysicalDepth int
	budget               PhysicalTraversalBudget
	closed               bool
}

type workingDirectoryCapability struct {
	mu            sync.Mutex
	authority     Authority
	platform      capturedRootPlatform
	selectedRoot  string
	selectionMode rootSelectionMode
	closed        bool
}

func (capability *workingDirectoryCapability) Validate() error {
	if capability == nil {
		return newFailure(FailureRootUnavailable, "", "working-directory capability is required", nil)
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	return capability.validateLocked()
}

func (capability *workingDirectoryCapability) OpenDirectory() (*os.File, error) {
	return capability.openDirectory(nil)
}

func (capability *workingDirectoryCapability) OpenDirectoryBounded(
	budget PhysicalTraversalBudget,
) (*os.File, error) {
	if budget == nil {
		return nil, fmt.Errorf("working-directory open budget is required")
	}
	return capability.openDirectory(budget)
}

func (capability *workingDirectoryCapability) openDirectory(
	budget PhysicalTraversalBudget,
) (*os.File, error) {
	if capability == nil {
		return nil, newFailure(FailureRootUnavailable, "", "working-directory capability is required", nil)
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if budget != nil {
		if capability.selectedRoot != "" {
			return nil, fmt.Errorf("bounded working-directory open does not admit a selected-root re-resolution")
		}
		visits, err := capturedRootValidationPathComponents(&capability.platform)
		if err != nil {
			return nil, err
		}
		if err := budget.AdmitPathComponents(visits); err != nil {
			return nil, fmt.Errorf("admit working-directory open: %w", err)
		}
	}
	if err := capability.validateLocked(); err != nil {
		return nil, err
	}
	file, err := openCapturedRootDirectory(&capability.platform)
	if err != nil {
		return nil, newFailure(
			FailureRootUnavailable,
			capability.authority.physicalRoot,
			"duplicate working-directory descriptor",
			err,
		)
	}
	return file, nil
}

func (capability *workingDirectoryCapability) Close() error {
	if capability == nil {
		return nil
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if capability.closed {
		return nil
	}
	capability.closed = true
	return closeCapturedRootPlatform(&capability.platform)
}

func (capability *workingDirectoryCapability) rootedPathWorkingDirectoryCapability() {}

func (capability *workingDirectoryCapability) validateLocked() error {
	if capability.closed {
		return newFailure(
			FailureRootUnavailable,
			capability.authority.physicalRoot,
			"working-directory capability is closed",
			nil,
		)
	}
	if err := capability.authority.Validate(); err != nil {
		return err
	}
	if err := validateCapturedRootPlatform(&capability.platform); err != nil {
		return err
	}
	if capability.selectedRoot != "" {
		return validateSelectionAgainstAuthority(
			capability.authority,
			capability.selectedRoot,
			capability.selectionMode,
			0,
			nil,
		)
	}
	return nil
}

func (capability *commitCapability) Destination() Destination {
	if capability == nil {
		return Destination{}
	}
	return capability.destination
}

func (capability *commitCapability) Validate() error {
	if capability == nil {
		return newFailure(FailureRootUnavailable, "", "commit capability is required", nil)
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	return capability.validateLocked()
}

func (capability *commitCapability) OpenRootDirectory() (*os.File, error) {
	if capability == nil {
		return nil, newFailure(FailureRootUnavailable, "", "commit capability is required", nil)
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if capability.budget != nil {
		if err := ChargeDestinationPath(
			capability.destination,
			capability.maximumPhysicalDepth,
			capability.budget,
		); err != nil {
			return nil, fmt.Errorf("admit rooted destination operation: %w", err)
		}
	}
	if err := capability.validateLocked(); err != nil {
		return nil, err
	}
	file, err := openCapturedRootDirectory(&capability.platform)
	if err != nil {
		return nil, newFailure(
			FailureRootUnavailable,
			capability.destination.root.physicalRoot,
			"open read-only root descriptor",
			err,
		)
	}
	return file, nil
}

func (capability *commitCapability) OpenRootDirectoryForMutation() (*os.File, error) {
	if capability == nil {
		return nil, newFailure(FailureRootUnavailable, "", "commit capability is required", nil)
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if capability.budget != nil {
		if err := ChargeDestinationPath(
			capability.destination,
			capability.maximumPhysicalDepth,
			capability.budget,
		); err != nil {
			return nil, fmt.Errorf("admit rooted destination mutation: %w", err)
		}
	}
	if err := capability.validateLocked(); err != nil {
		return nil, err
	}
	file, err := openCapturedCommitRootDirectory(&capability.platform)
	if err != nil {
		return nil, newFailure(
			FailureRootUnavailable,
			capability.destination.root.physicalRoot,
			"open mutating root descriptor",
			err,
		)
	}
	return file, nil
}

func (capability *commitCapability) ValidateDirectoryHandle(handle uintptr) error {
	if capability == nil {
		return newFailure(FailureRootUnavailable, "", "commit capability is required", nil)
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if err := capability.validateLocked(); err != nil {
		return err
	}
	return validateCapturedDirectoryHandle(&capability.platform, handle)
}

func (capability *commitCapability) ValidateRetainedDirectoryHandle(handle uintptr) error {
	if capability == nil {
		return newFailure(FailureRootUnavailable, "", "commit capability is required", nil)
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if err := capability.validateStateLocked(); err != nil {
		return err
	}
	return validateCapturedDirectoryHandle(&capability.platform, handle)
}

func (capability *commitCapability) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if capability == nil {
		return newFailure(FailureRootUnavailable, "", "commit capability is required", nil)
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if err := capability.validateStateLocked(); err != nil {
		return err
	}
	if pathComponents < 0 || entries < 0 || bytes < 0 {
		return fmt.Errorf("commit physical work must not be negative")
	}
	if capability.budget == nil {
		return nil
	}
	budget, ok := capability.budget.(physicalWorkBudget)
	if !ok {
		if entries == 0 && bytes == 0 {
			return capability.budget.AdmitPathComponents(pathComponents)
		}
		return fmt.Errorf("bounded commit capability lacks physical-work capacity")
	}
	return budget.AdmitPhysicalWork(pathComponents, entries, bytes)
}

func (capability *commitCapability) Close() error {
	if capability == nil {
		return nil
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if capability.closed {
		return nil
	}
	capability.closed = true
	return closeCapturedRootPlatform(&capability.platform)
}

func (capability *commitCapability) rootedPathCommitCapability() {}

func (capability *commitCapability) validateLocked() error {
	if err := capability.validateStateLocked(); err != nil {
		return err
	}
	return validateCapturedRootPlatform(&capability.platform)
}

func (capability *commitCapability) validateStateLocked() error {
	if capability.closed {
		return newFailure(
			FailureRootUnavailable,
			capability.destination.root.physicalRoot,
			"commit capability is closed",
			nil,
		)
	}
	if err := capability.destination.Validate(); err != nil {
		return fmt.Errorf("validate commit destination: %w", err)
	}
	return nil
}
