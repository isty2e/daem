// Package rootedpath models retained physical-root authority for bounded path mutations.
package rootedpath

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

type identityToken [32]byte

// mountIdentities keeps operation-local and durable-recovery mount evidence
// separate. Operation identity is required for every captured authority;
// recovery evidence is interpreted only when durable provenance is requested.
type mountIdentities struct {
	operation identityToken
	recovery  recoveryMountEvidence
}

func newMountIdentities(operation identityToken, recovery recoveryMountEvidence) mountIdentities {
	return mountIdentities{operation: operation, recovery: recovery}
}

// Authority identifies one physical directory root captured by a native boundary
// adapter. The object and mount tokens are deliberately opaque outside this
// package.
type Authority struct {
	physicalRoot string
	object       identityToken
	mount        mountIdentities
}

// newCapturedAuthority is the sole ingress from native root observations into
// the canonical model. Platform adapters must establish the physical facts
// before calling it.
func newCapturedAuthority(physicalRoot string, object identityToken, mount mountIdentities) (Authority, error) {
	authority := Authority{physicalRoot: physicalRoot, object: object, mount: mount}
	if err := authority.Validate(); err != nil {
		return Authority{}, err
	}
	return authority, nil
}

// Validate checks canonical authority facts without touching the filesystem.
func (authority Authority) Validate() error {
	if err := validatePhysicalRoot(authority.physicalRoot); err != nil {
		return err
	}
	if authority.object == (identityToken{}) {
		return newFailure(FailureInvalidRoot, authority.physicalRoot, "physical root object identity is required", nil)
	}
	if authority.mount.operation == (identityToken{}) {
		return newFailure(FailureInvalidRoot, authority.physicalRoot, "physical root mount identity is required", nil)
	}
	return nil
}

func validatePhysicalRoot(physicalRoot string) error {
	if strings.TrimSpace(physicalRoot) == "" {
		return newFailure(FailureInvalidRoot, physicalRoot, "physical root is required", nil)
	}
	if strings.IndexFunc(physicalRoot, isForbiddenPathRune) >= 0 {
		return newFailure(FailureInvalidRoot, physicalRoot, "physical root contains a control character", nil)
	}
	if !filepath.IsAbs(physicalRoot) {
		return newFailure(FailureInvalidRoot, physicalRoot, "physical root must be absolute", nil)
	}
	if filepath.Clean(physicalRoot) != physicalRoot {
		return newFailure(FailureInvalidRoot, physicalRoot, "physical root must be clean", nil)
	}
	if filepath.Dir(physicalRoot) == physicalRoot {
		return newFailure(FailureInvalidRoot, physicalRoot, "filesystem root cannot be mutation authority", nil)
	}
	return validatePlatformPhysicalRoot(physicalRoot)
}

// PhysicalRoot returns the canonical physical spelling captured at ingress.
// It is suitable for diagnostics and identity derivation, not as commit authority.
func (authority Authority) PhysicalRoot() string {
	return authority.physicalRoot
}

// Equal reports whether two values identify the same captured physical root incarnation.
func (authority Authority) Equal(other Authority) bool {
	return authority.Validate() == nil && other.Validate() == nil &&
		authority.physicalRoot == other.physicalRoot &&
		authority.object == other.object && authority.mount.operation == other.mount.operation
}

// Bind constructs a canonical destination under this authority.
func (authority Authority) Bind(relative RelativeDestination) (Destination, error) {
	if err := authority.Validate(); err != nil {
		return Destination{}, err
	}
	if err := relative.Validate(); err != nil {
		return Destination{}, err
	}
	return Destination{root: authority, relative: relative}, nil
}

// RelativeDestination is a canonical slash-separated path beneath a captured root.
type RelativeDestination struct {
	value string
}

// NewRelativeDestination normalizes a root-relative destination at ingress.
func NewRelativeDestination(value string) (RelativeDestination, error) {
	if strings.TrimSpace(value) == "" {
		return RelativeDestination{}, newFailure(FailureInvalidDestination, value, "destination is required", nil)
	}
	if strings.Contains(value, "\\") {
		return RelativeDestination{}, newFailure(
			FailureInvalidDestination,
			value,
			"destination must use slash-separated path components",
			nil,
		)
	}
	if strings.IndexFunc(value, isForbiddenPathRune) >= 0 {
		return RelativeDestination{}, newFailure(FailureInvalidDestination, value, "destination contains a control character", nil)
	}
	if path.IsAbs(value) || hasPathVolumePrefix(value) || value == "~" || strings.HasPrefix(value, "~/") {
		return RelativeDestination{}, newFailure(FailureInvalidDestination, value, "destination must be root-relative", nil)
	}
	for _, component := range strings.Split(value, "/") {
		if component == ".." {
			return RelativeDestination{}, newFailure(FailureInvalidDestination, value, "destination contains a parent traversal", nil)
		}
	}

	canonical := path.Clean(value)
	if canonical == "." || canonical == ".." || strings.HasPrefix(canonical, "../") {
		return RelativeDestination{}, newFailure(FailureInvalidDestination, value, "destination must name an entry below the captured root", nil)
	}
	if err := validatePlatformRelativeDestination(canonical); err != nil {
		return RelativeDestination{}, err
	}
	relative := RelativeDestination{value: canonical}
	if err := relative.Validate(); err != nil {
		return RelativeDestination{}, err
	}
	return relative, nil
}

// Validate checks canonical relative-destination invariants.
func (relative RelativeDestination) Validate() error {
	if relative.value == "" || relative.value == "." {
		return newFailure(FailureInvalidDestination, relative.value, "destination is not initialized", nil)
	}
	if strings.Contains(relative.value, "\\") || strings.IndexFunc(relative.value, isForbiddenPathRune) >= 0 ||
		path.IsAbs(relative.value) || hasPathVolumePrefix(relative.value) ||
		relative.value == "~" || strings.HasPrefix(relative.value, "~/") || path.Clean(relative.value) != relative.value ||
		relative.value == ".." || strings.HasPrefix(relative.value, "../") {
		return newFailure(FailureInvalidDestination, relative.value, "destination is not canonical", nil)
	}
	for _, component := range strings.Split(relative.value, "/") {
		if component == ".." {
			return newFailure(FailureInvalidDestination, relative.value, "destination contains a parent traversal", nil)
		}
	}
	return validatePlatformRelativeDestination(relative.value)
}

// Path returns the canonical slash-separated relative path.
func (relative RelativeDestination) Path() string {
	return relative.value
}

// Equal reports canonical relative-path equality.
func (relative RelativeDestination) Equal(other RelativeDestination) bool {
	return relative.Validate() == nil && other.Validate() == nil && relative.value == other.value
}

// Destination binds one canonical relative path to one captured root authority.
type Destination struct {
	root     Authority
	relative RelativeDestination
}

// CommitCapability is an ephemeral, non-serializable native handle binding one
// destination to its captured root. Only this package's platform adapter may
// implement it; storage adapters consume it during one commit attempt.
type CommitCapability interface {
	Destination() Destination
	Validate() error
	OpenRootDirectory() (*os.File, error)
	ValidateDirectoryHandle(handle uintptr) error
	ValidateRetainedDirectoryHandle(handle uintptr) error
	AdmitPhysicalWork(pathComponents int, entries int, bytes int64) error
	Close() error
	rootedPathCommitCapability()
}

// WorkingDirectoryCapability is an ephemeral, non-serializable native handle
// for launching one process from the captured physical root. It grants no
// destination mutation or process-sandbox authority.
type WorkingDirectoryCapability interface {
	Validate() error
	OpenDirectory() (*os.File, error)
	OpenDirectoryBounded(budget PhysicalTraversalBudget) (*os.File, error)
	Close() error
	rootedPathWorkingDirectoryCapability()
}

// Validate checks that both destination components are initialized and canonical.
func (destination Destination) Validate() error {
	if err := destination.root.Validate(); err != nil {
		return err
	}
	return destination.relative.Validate()
}

// Root returns the captured root authority.
func (destination Destination) Root() Authority {
	return destination.root
}

// Relative returns the canonical path beneath the captured root.
func (destination Destination) Relative() RelativeDestination {
	return destination.relative
}

// ParentChainValidationWork returns the component work for one bounded
// destination-parent validation. The gate verifies the retained physical root
// before and after checking every root-relative parent binding.
func (destination Destination) ParentChainValidationWork() (int, error) {
	if err := destination.Validate(); err != nil {
		return 0, err
	}
	rootDepth, err := absolutePathDepth(destination.root.physicalRoot)
	if err != nil {
		return 0, err
	}
	parentDepth := strings.Count(destination.relative.value, "/")
	maximumInt := int(^uint(0) >> 1)
	if rootDepth > (maximumInt-parentDepth)/2 {
		return 0, fmt.Errorf("destination parent-chain validation work overflows")
	}
	return 2*rootDepth + parentDepth, nil
}

// LexicalPath returns the physical-root spelling joined with the relative path.
// It is diagnostic and lease-domain input only; it is never a commit capability.
func (destination Destination) LexicalPath() (string, error) {
	if err := destination.Validate(); err != nil {
		return "", err
	}
	joined := filepath.Clean(filepath.Join(destination.root.physicalRoot, filepath.FromSlash(destination.relative.value)))
	relative, err := filepath.Rel(destination.root.physicalRoot, joined)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", newFailure(FailureInvalidDestination, destination.relative.value, "destination escaped its root", err)
	}
	return joined, nil
}

// Equal reports equality of root incarnation and canonical relative destination.
func (destination Destination) Equal(other Destination) bool {
	return destination.Validate() == nil && other.Validate() == nil &&
		destination.root.Equal(other.root) && destination.relative.Equal(other.relative)
}

func isForbiddenPathRune(value rune) bool {
	return value == '\x00' || unicode.IsControl(value)
}

func hasPathVolumePrefix(value string) bool {
	if filepath.VolumeName(filepath.FromSlash(value)) != "" {
		return true
	}
	return len(value) >= 2 && isASCIIAlpha(value[0]) && value[1] == ':'
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
