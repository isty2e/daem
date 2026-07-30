package rootedpath

import (
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

func captureRoot(selectedRoot string, selectionMode rootSelectionMode) (*CapturedRoot, error) {
	physicalRoot, platform, object, mount, err := captureRootPlatform(selectedRoot, selectionMode)
	if err != nil {
		return nil, err
	}
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

// CaptureDestination binds one absolute destination to the nearest existing
// directory ancestor and retains that ancestor's native witness. Missing
// descendants remain root-relative names, so later mutation never resolves
// the selected path through ambient ancestors again.
func CaptureDestination(path string) (*CapturedRoot, Destination, error) {
	if strings.TrimSpace(path) == "" {
		return nil, Destination{}, newFailure(FailureInvalidDestination, path, "destination is required", nil)
	}
	if strings.IndexFunc(path, isForbiddenPathRune) >= 0 {
		return nil, Destination{}, newFailure(FailureInvalidDestination, path, "destination contains a control character", nil)
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, Destination{}, newFailure(FailureInvalidDestination, path, "resolve destination", err)
	}
	if filepath.Dir(absolute) == absolute {
		return nil, Destination{}, newFailure(FailureInvalidDestination, absolute, "destination must name an entry below a capturable directory", nil)
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
			root, captureErr := CaptureRoot(ancestor)
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

// ValidateSelection verifies that selectedRoot still resolves to the physical
// root incarnation retained by this witness.
func (root *CapturedRoot) ValidateSelection(selectedRoot string) error {
	expected, err := root.Authority()
	if err != nil {
		return err
	}
	return validateSelectionAgainstAuthority(expected, selectedRoot, root.selectionMode)
}

func validateSelectionAgainstAuthority(
	expected Authority,
	selectedRoot string,
	selectionMode rootSelectionMode,
) error {
	current, err := captureRoot(selectedRoot, selectionMode)
	if err != nil {
		return err
	}
	defer current.Close()
	observed, err := current.Authority()
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
	if expected.mount != observed.mount {
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
	if err := validateCapturedRootPlatform(&root.platform); err != nil {
		return nil, err
	}
	platform, err := cloneCapturedRootPlatform(&root.platform)
	if err != nil {
		return nil, newFailure(FailureRootUnavailable, root.authority.physicalRoot, "duplicate captured root witness", err)
	}
	capability := &commitCapability{destination: destination, platform: platform}
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
	return root.acquireWorkingDirectory("")
}

// AcquireSelectedWorkingDirectory issues a process-directory capability that
// also rejects later retargeting of the selected root path.
func (root *CapturedRoot) AcquireSelectedWorkingDirectory(selectedRoot string) (WorkingDirectoryCapability, error) {
	if err := root.ValidateSelection(selectedRoot); err != nil {
		return nil, err
	}
	return root.acquireWorkingDirectory(selectedRoot)
}

func (root *CapturedRoot) acquireWorkingDirectory(selectedRoot string) (WorkingDirectoryCapability, error) {
	if root == nil {
		return nil, newFailure(FailureRootUnavailable, "", "captured root is required", nil)
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.closed {
		return nil, newFailure(FailureRootUnavailable, root.authority.physicalRoot, "captured root is closed", nil)
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
	mu          sync.Mutex
	destination Destination
	platform    capturedRootPlatform
	closed      bool
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
	if capability == nil {
		return nil, newFailure(FailureRootUnavailable, "", "working-directory capability is required", nil)
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
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
	if err := capability.validateLocked(); err != nil {
		return nil, err
	}
	file, err := openCapturedRootDirectory(&capability.platform)
	if err != nil {
		return nil, newFailure(
			FailureRootUnavailable,
			capability.destination.root.physicalRoot,
			"duplicate commit root descriptor",
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
	return validateCapturedRootPlatform(&capability.platform)
}
