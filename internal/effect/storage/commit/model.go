package commit

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/residue"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type entryKind uint8

const (
	entryKindInvalid entryKind = iota
	entryKindRegular
	entryKindDirectory
	entryKindSymlink
	entryKindSpecial
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
	case entryKindSpecial:
		return mutationfs.EntryKindSpecial
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
	path           string
	payload        []byte
	mode           fs.FileMode
	policy         filePolicy
	expected       EntryIdentity
	expectedParent EntryIdentity
	capability     rootedpath.CommitCapability
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
	residue    *residue.LogicalRemovalResidueName
}

// RootedEntryRename is one exact, no-replace same-parent namespace transition.
// The destination name has no storage or domain meaning at this boundary.
type RootedEntryRename struct {
	sourcePath      string
	destinationName string
	expected        EntryIdentity
	capability      rootedpath.CommitCapability
}

// NewRootedEntryRename constructs an identity-guarded sibling rename. The
// caller retains capability ownership on construction error; commit consumes
// it on every attempt.
func NewRootedEntryRename(
	capability rootedpath.CommitCapability,
	destinationName string,
	expected EntryIdentity,
) (RootedEntryRename, error) {
	sourcePath, err := rootedCapabilityPath(capability)
	if err != nil {
		return RootedEntryRename{}, err
	}
	if err := validateSiblingName(destinationName); err != nil {
		return RootedEntryRename{}, fmt.Errorf("destination name: %w", err)
	}
	if filepath.Base(sourcePath) == destinationName {
		return RootedEntryRename{}, fmt.Errorf("source and destination names must differ")
	}
	if !expected.valid() || expected.path != sourcePath {
		return RootedEntryRename{}, fmt.Errorf("expected identity must describe %q", sourcePath)
	}
	switch expected.kind {
	case entryKindRegular, entryKindDirectory:
	default:
		return RootedEntryRename{}, fmt.Errorf("expected identity has unsupported entry kind")
	}
	return RootedEntryRename{
		sourcePath:      sourcePath,
		destinationName: destinationName,
		expected:        expected,
		capability:      capability,
	}, nil
}

// RootedEntryCleanup is one exact identity-guarded removal under retained-root
// authority. It does not create an intermediate tombstone.
type RootedEntryCleanup struct {
	path       string
	expected   EntryIdentity
	capability rootedpath.CommitCapability
}

// NewRootedEntryCleanup constructs exact cleanup of a regular file or
// directory. The caller retains capability ownership on construction error;
// commit consumes it on every attempt.
func NewRootedEntryCleanup(
	capability rootedpath.CommitCapability,
	expected EntryIdentity,
) (RootedEntryCleanup, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return RootedEntryCleanup{}, err
	}
	if !expected.valid() || expected.path != path {
		return RootedEntryCleanup{}, fmt.Errorf("expected identity must describe %q", path)
	}
	switch expected.kind {
	case entryKindRegular, entryKindDirectory:
	default:
		return RootedEntryCleanup{}, fmt.Errorf("expected identity has unsupported entry kind")
	}
	return RootedEntryCleanup{
		path:       path,
		expected:   expected,
		capability: capability,
	}, nil
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

// NewRootedLogicalRemovalWithResidue constructs a journal-authorized rooted
// removal. The caller-selected residue is opaque storage syntax; storage never
// derives it from an operation, resource, or action ordinal.
func NewRootedLogicalRemovalWithResidue(
	capability rootedpath.CommitCapability,
	expected EntryIdentity,
	residue residue.LogicalRemovalResidueName,
) (LogicalRemoval, error) {
	if !residue.Valid() {
		return LogicalRemoval{}, fmt.Errorf("logical removal residue name is invalid")
	}
	request, err := NewRootedLogicalRemoval(capability, expected)
	if err != nil {
		return LogicalRemoval{}, err
	}
	request.residue = &residue
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
	if err := validateRootedPath(path); err != nil {
		return err
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, temporaryPrefix) || strings.HasPrefix(base, tombstonePrefix) {
		return fmt.Errorf("path uses a reserved storage commit name")
	}
	return nil
}

func validateRootedPath(path string) error {
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
	return nil
}

func validateSiblingName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("sibling name must be a non-empty path component")
	}
	if strings.ContainsRune(name, '\x00') || filepath.Base(name) != name {
		return fmt.Errorf("sibling name must be one canonical path component")
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
	if err := validateRootedPath(path); err != nil {
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
