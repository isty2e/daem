package commit

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type entryKind uint8

const (
	entryKindInvalid entryKind = iota
	entryKindRegular
	entryKindDirectory
	entryKindSymlink
)

const (
	temporaryPrefix = ".daem-tmp-"
	tombstonePrefix = ".daem-tombstone-"
)

// EntryIdentity is ephemeral evidence for revalidating one directory entry.
// It is not a durable revision, lease, or atomic compare-and-swap token.
type EntryIdentity struct {
	path     string
	kind     entryKind
	platform platformIdentity
}

func (identity EntryIdentity) valid() bool {
	return identity.path != "" && identity.kind != entryKindInvalid && identity.platform.valid()
}

func (identity EntryIdentity) sameEntry(other EntryIdentity) bool {
	return identity.valid() && other.valid() && identity.kind == other.kind &&
		identity.platform.matches(other.platform)
}

// Equal reports whether two observations identify the same no-follow entry at
// the same diagnostic path. It grants no authority to mutate that path.
func (identity EntryIdentity) Equal(other mutationfs.EntryIdentity) bool {
	concrete, ok := other.(EntryIdentity)
	return ok && identity.path == concrete.path && identity.sameEntry(concrete)
}

// Kind returns the no-follow structural form captured by this identity.
func (identity EntryIdentity) Kind() mutationfs.EntryKind {
	switch identity.kind {
	case entryKindRegular:
		return mutationfs.EntryKindFile
	case entryKindDirectory:
		return mutationfs.EntryKindDirectory
	case entryKindSymlink:
		return mutationfs.EntryKindSymlink
	default:
		return mutationfs.EntryKindInvalid
	}
}

// sameObject is narrower evidence for an inode kept alive by an open handle or
// continuous link across a daem-owned rename or child mutation that changes ctime.
func (identity EntryIdentity) sameObject(other EntryIdentity) bool {
	return identity.valid() && other.valid() && identity.kind == other.kind &&
		identity.platform.sameObject(other.platform)
}

type filePolicy uint8

const (
	filePolicyInvalid filePolicy = iota
	filePolicyMustBeAbsent
	filePolicyReplaceExpected
)

// FileCommit is a valid file publication request.
type FileCommit struct {
	path       string
	payload    []byte
	mode       fs.FileMode
	policy     filePolicy
	expected   EntryIdentity
	capability rootedpath.CommitCapability
}

// NewRootedFileCreate constructs an exclusive file-creation request bound to
// one physical rooted-path capability. On success, CommitFile owns and
// consumes the capability; on error, the caller retains ownership.
func NewRootedFileCreate(
	capability rootedpath.CommitCapability,
	payload []byte,
	mode fs.FileMode,
) (FileCommit, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return FileCommit{}, err
	}
	request, err := NewFileCreate(path, payload, mode)
	if err != nil {
		return FileCommit{}, err
	}
	request.capability = capability
	return request, nil
}

// NewFileCreate constructs an exclusive file-creation request.
func NewFileCreate(path string, payload []byte, mode fs.FileMode) (FileCommit, error) {
	if err := validateCommitPath(path); err != nil {
		return FileCommit{}, err
	}
	if err := validateFileMode(mode); err != nil {
		return FileCommit{}, err
	}
	return FileCommit{
		path:    path,
		payload: append([]byte(nil), payload...),
		mode:    mode,
		policy:  filePolicyMustBeAbsent,
	}, nil
}

// NewFileReplacement constructs a replacement guarded by the expected regular
// file identity.
func NewFileReplacement(
	path string,
	payload []byte,
	mode fs.FileMode,
	expected EntryIdentity,
) (FileCommit, error) {
	if err := validateCommitPath(path); err != nil {
		return FileCommit{}, err
	}
	if err := validateFileMode(mode); err != nil {
		return FileCommit{}, err
	}
	if err := validateExpectedIdentity(path, expected, entryKindRegular); err != nil {
		return FileCommit{}, err
	}
	return FileCommit{
		path:     path,
		payload:  append([]byte(nil), payload...),
		mode:     mode,
		policy:   filePolicyReplaceExpected,
		expected: expected,
	}, nil
}

// NewRootedFileReplacement constructs an identity-guarded replacement bound
// to one physical rooted-path capability. On success, CommitFile owns and
// consumes the capability; on error, the caller retains ownership.
func NewRootedFileReplacement(
	capability rootedpath.CommitCapability,
	payload []byte,
	mode fs.FileMode,
	expected EntryIdentity,
) (FileCommit, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return FileCommit{}, err
	}
	if expected.kind == entryKindSymlink {
		return FileCommit{}, rootedFinalSymlinkFailure(path)
	}
	request, err := NewFileReplacement(path, payload, mode, expected)
	if err != nil {
		return FileCommit{}, err
	}
	request.capability = capability
	return request, nil
}

// PreparedTreeCommit is a valid same-parent, no-replace tree publication.
type PreparedTreeCommit struct {
	stagedRoot  string
	destination string
	expected    EntryIdentity
}

// NewPreparedTreeCommit constructs publication of an already prepared tree.
func NewPreparedTreeCommit(
	stagedRoot string,
	destination string,
	expected EntryIdentity,
) (PreparedTreeCommit, error) {
	if err := validateCommitPath(stagedRoot); err != nil {
		return PreparedTreeCommit{}, fmt.Errorf("staged root: %w", err)
	}
	if err := validateCommitPath(destination); err != nil {
		return PreparedTreeCommit{}, fmt.Errorf("destination: %w", err)
	}
	if stagedRoot == destination {
		return PreparedTreeCommit{}, fmt.Errorf("staged root and destination must differ")
	}
	if filepath.Dir(stagedRoot) != filepath.Dir(destination) {
		return PreparedTreeCommit{}, fmt.Errorf("staged root and destination must have the same parent")
	}
	if err := validateExpectedIdentity(stagedRoot, expected, entryKindDirectory); err != nil {
		return PreparedTreeCommit{}, err
	}
	return PreparedTreeCommit{
		stagedRoot:  stagedRoot,
		destination: destination,
		expected:    expected,
	}, nil
}

// LogicalRemoval is a valid identity-guarded directory-entry removal.
type LogicalRemoval struct {
	path       string
	expected   EntryIdentity
	capability rootedpath.CommitCapability
}

// NewRootedLogicalRemoval constructs a rooted removal of an expected regular
// file or directory. Rooted final-entry symlinks are never admitted. On
// success, CommitLogicalRemoval owns and consumes the capability; on error,
// the caller retains ownership.
func NewRootedLogicalRemoval(
	capability rootedpath.CommitCapability,
	expected EntryIdentity,
) (LogicalRemoval, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return LogicalRemoval{}, err
	}
	if expected.kind == entryKindSymlink {
		return LogicalRemoval{}, rootedFinalSymlinkFailure(path)
	}
	request, err := NewLogicalRemoval(path, expected)
	if err != nil {
		return LogicalRemoval{}, err
	}
	request.capability = capability
	return request, nil
}

// NewLogicalRemoval constructs removal of the expected regular file,
// directory, or symbolic-link entry.
func NewLogicalRemoval(path string, expected EntryIdentity) (LogicalRemoval, error) {
	if err := validateCommitPath(path); err != nil {
		return LogicalRemoval{}, err
	}
	if !expected.valid() || expected.path != path {
		return LogicalRemoval{}, fmt.Errorf("expected identity must describe %q", path)
	}
	switch expected.kind {
	case entryKindRegular, entryKindDirectory, entryKindSymlink:
	default:
		return LogicalRemoval{}, fmt.Errorf("expected identity has unsupported entry kind")
	}
	return LogicalRemoval{path: path, expected: expected}, nil
}

func validateExpectedIdentity(path string, expected EntryIdentity, kind entryKind) error {
	if !expected.valid() || expected.path != path {
		return fmt.Errorf("expected identity must describe %q", path)
	}
	if expected.kind != kind {
		return fmt.Errorf("expected identity has incompatible entry kind")
	}
	return nil
}

func validateCommitPath(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("path must not contain NUL")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}
	if filepath.Clean(path) != path {
		return fmt.Errorf("path must be clean")
	}
	if filepath.Dir(path) == path {
		return fmt.Errorf("filesystem root is not a valid commit path")
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, temporaryPrefix) || strings.HasPrefix(base, tombstonePrefix) {
		return fmt.Errorf("path uses a reserved storage commit name")
	}
	return nil
}

func validateFileMode(mode fs.FileMode) error {
	if mode&^fs.ModePerm != 0 {
		return fmt.Errorf("file mode must contain permission bits only")
	}
	return nil
}

func rootedCapabilityPath(capability rootedpath.CommitCapability) (string, error) {
	if capability == nil {
		return "", fmt.Errorf("rooted commit capability is required")
	}
	if err := capability.Validate(); err != nil {
		return "", err
	}
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return "", err
	}
	if err := validateCommitPath(path); err != nil {
		return "", err
	}
	return path, nil
}

func validateRootedCapability(path string, capability rootedpath.CommitCapability) error {
	if capability == nil {
		return nil
	}
	capabilityPath, err := rootedCapabilityPath(capability)
	if err != nil {
		return err
	}
	if capabilityPath != path {
		return fmt.Errorf("rooted commit capability describes %q, not %q", capabilityPath, path)
	}
	return nil
}

func rootedFinalSymlinkFailure(path string) error {
	return rootedpath.NewBoundaryFailure(
		rootedpath.FailureFinalSymlink,
		path,
		"rooted destination final entry is a symbolic link",
		nil,
	)
}
