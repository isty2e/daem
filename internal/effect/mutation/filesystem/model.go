package filesystem

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
)

// EntryKind classifies the no-follow form represented by EntryIdentity.
type EntryKind string

const (
	EntryKindInvalid   EntryKind = ""
	EntryKindFile      EntryKind = "file"
	EntryKindDirectory EntryKind = "directory"
	EntryKindSymlink   EntryKind = "symlink"
	EntryKindSpecial   EntryKind = "special"
)

// EntryIdentity is operation-local evidence returned by a filesystem
// boundary. It grants no mutation authority and has no durable representation.
type EntryIdentity interface {
	Equal(EntryIdentity) bool
	Kind() EntryKind
}

// TreeStructureLimits bounds one recursive filesystem tree shape. Entry
// cardinality is global across descendants and excludes the selected root.
// Depth counts directories below the selected root.
type TreeStructureLimits struct {
	maximumEntries int
	maximumDepth   int
	initialized    bool
}

// NewTreeStructureLimits constructs finite tree-shape bounds. Zero entries is
// an exact empty-tree ceiling; zero depth permits only regular files directly
// below root.
func NewTreeStructureLimits(maximumEntries int, maximumDepth int) (TreeStructureLimits, error) {
	if maximumEntries < 0 {
		return TreeStructureLimits{}, fmt.Errorf(
			"tree structure maximum entries must not be negative",
		)
	}
	if maximumDepth < 0 {
		return TreeStructureLimits{}, fmt.Errorf(
			"tree structure maximum depth must not be negative",
		)
	}
	return TreeStructureLimits{
		maximumEntries: maximumEntries,
		maximumDepth:   maximumDepth,
		initialized:    true,
	}, nil
}

// MaximumEntries returns the global descendant-entry bound.
func (limits TreeStructureLimits) MaximumEntries() int { return limits.maximumEntries }

// MaximumDepth returns the maximum descendant-directory depth.
func (limits TreeStructureLimits) MaximumDepth() int { return limits.maximumDepth }

const (
	defaultTreeTraversalMaximumEntries = 100_000
	defaultTreeTraversalMaximumDepth   = 64
	defaultTreeTraversalMaximumBytes   = 4 << 30
)

// TreeTraversalLimits adds a regular-file byte bound to one tree-shape limit.
type TreeTraversalLimits struct {
	structure    TreeStructureLimits
	maximumBytes int64
	initialized  bool
}

// DefaultTreeTraversalLimits is the rooted-tree publication capability:
// 100,000 descendant entries, 64 descendant-directory levels, and 4 GiB of
// regular-file bytes. Identity, freshness, copy, staging, and publication share
// this inclusive per-tree contract. Operation-wide envelopes stay separate.
func DefaultTreeTraversalLimits() TreeTraversalLimits {
	limits, err := NewTreeTraversalLimits(
		defaultTreeTraversalMaximumEntries,
		defaultTreeTraversalMaximumDepth,
		defaultTreeTraversalMaximumBytes,
	)
	if err != nil {
		panic(err)
	}
	return limits
}

// RootedCleanupWorkEnvelope is the complete storage work admitted for one
// exact rooted cleanup. EntryWork includes snapshot capture, every whole-tree
// seal, destructive traversal, and one overflow-name probe. ByteWork covers
// the three passes that account regular-file sizes. PathWork covers every
// destination-parent chain check performed by those phases and the surrounding
// commit lifecycle.
type RootedCleanupWorkEnvelope struct {
	entryWork            int
	byteWork             int64
	namespaceValidations int
}

// NewRootedCleanupWorkEnvelope derives the fixed cleanup algorithm envelope
// from one exact root kind and its per-pass traversal limits.
func NewRootedCleanupWorkEnvelope(
	kind EntryKind,
	limits TreeTraversalLimits,
) (RootedCleanupWorkEnvelope, error) {
	if err := limits.Validate(); err != nil {
		return RootedCleanupWorkEnvelope{}, fmt.Errorf("rooted cleanup traversal limits: %w", err)
	}
	maximumInt := int(^uint(0) >> 1)
	switch kind {
	case EntryKindFile:
		if limits.MaximumBytes() > int64(^uint64(0)>>1)/3 {
			return RootedCleanupWorkEnvelope{}, fmt.Errorf(
				"rooted file cleanup byte envelope overflows",
			)
		}
		return RootedCleanupWorkEnvelope{
			byteWork:             limits.MaximumBytes() * 3,
			namespaceValidations: 8,
		}, nil
	case EntryKindDirectory:
		// Capture performs two logical descendant passes, the pre-effect seal
		// performs three, and destructive traversal performs two. Any
		// over-limit observation aborts at its first extra directory name, so
		// the complete operation needs one additional probe entry rather than
		// one per pass.
		if limits.MaximumEntries() > (maximumInt-1)/7 {
			return RootedCleanupWorkEnvelope{}, fmt.Errorf(
				"rooted directory cleanup entry envelope overflows",
			)
		}
		// Let N be descendant entries, D directories including the root, and L
		// non-directory leaves. Mode repair and destructive preparation perform
		// at most three authority checks per directory together; the remaining
		// destructive phase performs 1+3D+N+L checks. Every authority check
		// validates the destination-parent chain twice, and parent setup plus
		// the surrounding lifecycle add four checks. Since L=N-D+1 and
		// D <= N+1, the maximum is 14N+18.
		if limits.MaximumEntries() > (maximumInt-18)/14 {
			return RootedCleanupWorkEnvelope{}, fmt.Errorf(
				"rooted directory cleanup namespace envelope overflows",
			)
		}
		if limits.MaximumBytes() > int64(^uint64(0)>>1)/3 {
			return RootedCleanupWorkEnvelope{}, fmt.Errorf(
				"rooted directory cleanup byte envelope overflows",
			)
		}
		return RootedCleanupWorkEnvelope{
			entryWork:            7*limits.MaximumEntries() + 1,
			byteWork:             3 * limits.MaximumBytes(),
			namespaceValidations: 14*limits.MaximumEntries() + 18,
		}, nil
	default:
		return RootedCleanupWorkEnvelope{}, fmt.Errorf(
			"unsupported rooted cleanup entry kind %q",
			kind,
		)
	}
}

// EntryWork returns aggregate recursive descendant and overflow-probe work.
func (envelope RootedCleanupWorkEnvelope) EntryWork() int { return envelope.entryWork }

// ByteWork returns aggregate regular-file size work across cleanup phases.
func (envelope RootedCleanupWorkEnvelope) ByteWork() int64 { return envelope.byteWork }

// PathWork returns aggregate component work for every cleanup namespace gate.
func (envelope RootedCleanupWorkEnvelope) PathWork(parentValidationWork int) (int, error) {
	if parentValidationWork < 0 {
		return 0, fmt.Errorf("rooted cleanup parent-validation work must not be negative")
	}
	maximumInt := int(^uint(0) >> 1)
	if parentValidationWork != 0 && envelope.namespaceValidations > maximumInt/parentValidationWork {
		return 0, fmt.Errorf("rooted cleanup path-work envelope overflows")
	}
	return envelope.namespaceValidations * parentValidationWork, nil
}

// NewTreeTraversalLimits constructs finite traversal bounds. Zero entries or
// bytes represent an exact empty-content ceiling; zero depth permits only
// regular files directly below the selected root.
func NewTreeTraversalLimits(
	maximumEntries int,
	maximumDepth int,
	maximumBytes int64,
) (TreeTraversalLimits, error) {
	structure, err := NewTreeStructureLimits(maximumEntries, maximumDepth)
	if err != nil {
		return TreeTraversalLimits{}, err
	}
	if maximumBytes < 0 {
		return TreeTraversalLimits{}, fmt.Errorf(
			"tree traversal maximum bytes must not be negative",
		)
	}
	return TreeTraversalLimits{
		structure:    structure,
		maximumBytes: maximumBytes,
		initialized:  true,
	}, nil
}

// MaximumEntries returns the global entry-cardinality bound.
func (limits TreeTraversalLimits) MaximumEntries() int {
	return limits.structure.MaximumEntries()
}

// MaximumDepth returns the maximum descendant-directory depth.
func (limits TreeTraversalLimits) MaximumDepth() int {
	return limits.structure.MaximumDepth()
}

// MaximumBytes returns the global regular-file byte bound.
func (limits TreeTraversalLimits) MaximumBytes() int64 {
	return limits.maximumBytes
}

// Validate rejects an uninitialized traversal limit.
func (limits TreeTraversalLimits) Validate() error {
	if !limits.initialized || !limits.structure.initialized {
		return fmt.Errorf("tree traversal limits are uninitialized")
	}
	_, err := NewTreeTraversalLimits(
		limits.MaximumEntries(),
		limits.MaximumDepth(),
		limits.maximumBytes,
	)
	return err
}

// CommitOutcomeState is the strongest namespace conclusion established by one
// guarded commit attempt. It describes storage visibility only; semantic
// owners decide what the visible entry means.
type CommitOutcomeState string

const (
	CommitOutcomeUncommitted         CommitOutcomeState = "uncommitted"
	CommitOutcomeIndeterminate       CommitOutcomeState = "indeterminate"
	CommitOutcomeRetainedRecoverable CommitOutcomeState = "retained_recoverable"
	CommitOutcomeComplete            CommitOutcomeState = "complete"
)

// CommitOutcome reports stable storage state without exposing physical paths
// or platform errno policy. RetainedNames are same-parent entry names that a
// semantic owner may classify through fresh observation.
type CommitOutcome struct {
	state         CommitOutcomeState
	retainedNames []string
}

// NewCommitOutcome constructs one canonical storage conclusion.
func NewCommitOutcome(
	state CommitOutcomeState,
	retainedNames []string,
) (CommitOutcome, error) {
	switch state {
	case CommitOutcomeUncommitted, CommitOutcomeIndeterminate,
		CommitOutcomeRetainedRecoverable, CommitOutcomeComplete:
	default:
		return CommitOutcome{}, fmt.Errorf("unsupported commit outcome state %q", state)
	}

	names := slices.Clone(retainedNames)
	slices.Sort(names)
	for index, name := range names {
		if name == "" || name == "." || name == ".." ||
			strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\x00') {
			return CommitOutcome{}, fmt.Errorf(
				"commit outcome retained name %q is not one path component",
				name,
			)
		}
		if index > 0 && names[index-1] == name {
			return CommitOutcome{}, fmt.Errorf(
				"commit outcome contains duplicate retained name %q",
				name,
			)
		}
	}
	switch state {
	case CommitOutcomeUncommitted, CommitOutcomeComplete:
		if len(names) != 0 {
			return CommitOutcome{}, fmt.Errorf(
				"commit outcome %q cannot retain entries",
				state,
			)
		}
	case CommitOutcomeRetainedRecoverable:
		if len(names) == 0 {
			return CommitOutcome{}, fmt.Errorf(
				"commit outcome %q requires retained entries",
				state,
			)
		}
	}
	return CommitOutcome{state: state, retainedNames: names}, nil
}

// State returns the strongest established namespace conclusion.
func (outcome CommitOutcome) State() CommitOutcomeState {
	return outcome.state
}

// RetainedNames returns an owned copy in lexical order.
func (outcome CommitOutcome) RetainedNames() []string {
	return slices.Clone(outcome.retainedNames)
}

// RegularFileSnapshot is immutable content and mode from one identity-stable,
// no-follow regular-file read.
type RegularFileSnapshot struct {
	content  []byte
	mode     fs.FileMode
	identity EntryIdentity
}

// DirectoryEntrySnapshot is one immutable no-follow observation immediately
// below a directory. Identity is ephemeral evidence, not mutation authority.
type DirectoryEntrySnapshot struct {
	name     string
	identity EntryIdentity
	mode     fs.FileMode
	owned    bool
	size     int64
}

// NewDirectoryEntrySnapshot constructs one normalized directory-entry
// observation from boundary-established facts.
func NewDirectoryEntrySnapshot(
	name string,
	identity EntryIdentity,
	mode fs.FileMode,
	owned bool,
	size int64,
) (DirectoryEntrySnapshot, error) {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\x00') {
		return DirectoryEntrySnapshot{}, fmt.Errorf("directory entry name %q is not canonical", name)
	}
	if identity == nil || identity.Kind() == EntryKindInvalid {
		return DirectoryEntrySnapshot{}, fmt.Errorf("directory entry %q identity is required", name)
	}
	if mode&^fs.ModePerm != 0 {
		return DirectoryEntrySnapshot{}, fmt.Errorf("directory entry %q mode must contain permission bits only", name)
	}
	if size < 0 {
		return DirectoryEntrySnapshot{}, fmt.Errorf("directory entry %q size must not be negative", name)
	}
	return DirectoryEntrySnapshot{
		name:     name,
		identity: identity,
		mode:     mode.Perm(),
		owned:    owned,
		size:     size,
	}, nil
}

// Name returns the single observed entry component.
func (snapshot DirectoryEntrySnapshot) Name() string {
	return snapshot.name
}

// Identity returns ephemeral no-follow identity evidence.
func (snapshot DirectoryEntrySnapshot) Identity() EntryIdentity {
	return snapshot.identity
}

// Kind returns the no-follow structural form.
func (snapshot DirectoryEntrySnapshot) Kind() EntryKind {
	if snapshot.identity == nil {
		return EntryKindInvalid
	}
	return snapshot.identity.Kind()
}

// Mode returns observed permission bits.
func (snapshot DirectoryEntrySnapshot) Mode() fs.FileMode {
	return snapshot.mode
}

// OwnedByInvoker reports whether the entry owner matched the invoking user.
func (snapshot DirectoryEntrySnapshot) OwnedByInvoker() bool {
	return snapshot.owned
}

// Size returns the no-follow stat size.
func (snapshot DirectoryEntrySnapshot) Size() int64 {
	return snapshot.size
}

// DirectorySnapshot is one stable immediate-child inventory. It deliberately
// carries no traversal or artifact semantics.
type DirectorySnapshot struct {
	rootIdentity EntryIdentity
	rootMode     fs.FileMode
	rootOwned    bool
	entries      []DirectoryEntrySnapshot
	initialized  bool
}

// NewDirectorySnapshot constructs a canonical lexical directory inventory.
func NewDirectorySnapshot(
	rootIdentity EntryIdentity,
	rootMode fs.FileMode,
	rootOwned bool,
	entries []DirectoryEntrySnapshot,
) (DirectorySnapshot, error) {
	if rootIdentity == nil || rootIdentity.Kind() != EntryKindDirectory {
		return DirectorySnapshot{}, fmt.Errorf("directory snapshot root identity must describe a directory")
	}
	if rootMode&^fs.ModePerm != 0 {
		return DirectorySnapshot{}, fmt.Errorf("directory snapshot root mode must contain permission bits only")
	}
	cloned := append([]DirectoryEntrySnapshot(nil), entries...)
	slices.SortFunc(cloned, func(left DirectoryEntrySnapshot, right DirectoryEntrySnapshot) int {
		return strings.Compare(left.name, right.name)
	})
	for index, entry := range cloned {
		normalized, err := NewDirectoryEntrySnapshot(
			entry.name,
			entry.identity,
			entry.mode,
			entry.owned,
			entry.size,
		)
		if err != nil {
			return DirectorySnapshot{}, fmt.Errorf("directory snapshot entries[%d]: %w", index, err)
		}
		cloned[index] = normalized
		if index > 0 && cloned[index-1].name == normalized.name {
			return DirectorySnapshot{}, fmt.Errorf("directory snapshot contains duplicate entry %q", normalized.name)
		}
	}
	return DirectorySnapshot{
		rootIdentity: rootIdentity,
		rootMode:     rootMode.Perm(),
		rootOwned:    rootOwned,
		entries:      cloned,
		initialized:  true,
	}, nil
}

// RootIdentity returns the observed directory identity.
func (snapshot DirectorySnapshot) RootIdentity() EntryIdentity {
	if !snapshot.initialized {
		return nil
	}
	return snapshot.rootIdentity
}

// RootMode returns the observed directory permission bits.
func (snapshot DirectorySnapshot) RootMode() fs.FileMode {
	if !snapshot.initialized {
		return 0
	}
	return snapshot.rootMode
}

// RootOwnedByInvoker reports whether the root owner matched the invoking user.
func (snapshot DirectorySnapshot) RootOwnedByInvoker() bool {
	return snapshot.initialized && snapshot.rootOwned
}

// Entries returns an owned copy in lexical name order.
func (snapshot DirectorySnapshot) Entries() []DirectoryEntrySnapshot {
	if !snapshot.initialized {
		return nil
	}
	return append([]DirectoryEntrySnapshot(nil), snapshot.entries...)
}

// Equal reports whether two snapshots contain the same complete observation.
func (snapshot DirectorySnapshot) Equal(other DirectorySnapshot) bool {
	if !snapshot.initialized || !other.initialized ||
		snapshot.rootIdentity == nil || other.rootIdentity == nil ||
		!snapshot.rootIdentity.Equal(other.rootIdentity) ||
		snapshot.rootMode != other.rootMode ||
		snapshot.rootOwned != other.rootOwned ||
		len(snapshot.entries) != len(other.entries) {
		return false
	}
	for index, entry := range snapshot.entries {
		candidate := other.entries[index]
		if entry.name != candidate.name ||
			entry.identity == nil || candidate.identity == nil ||
			!entry.identity.Equal(candidate.identity) ||
			entry.mode != candidate.mode ||
			entry.owned != candidate.owned ||
			entry.size != candidate.size {
			return false
		}
	}
	return true
}

// NewRegularFileSnapshot constructs an immutable identity-bound file snapshot.
func NewRegularFileSnapshot(
	content []byte,
	mode fs.FileMode,
	identity EntryIdentity,
) (RegularFileSnapshot, error) {
	if identity == nil || identity.Kind() != EntryKindFile {
		return RegularFileSnapshot{}, fmt.Errorf("regular file snapshot requires a file identity")
	}
	if mode&^fs.ModePerm != 0 {
		return RegularFileSnapshot{}, fmt.Errorf("regular file snapshot mode must contain permission bits only")
	}
	return RegularFileSnapshot{
		content:  slices.Clone(content),
		mode:     mode.Perm(),
		identity: identity,
	}, nil
}

// Content returns an owned copy of the observed file bytes.
func (snapshot RegularFileSnapshot) Content() []byte {
	return slices.Clone(snapshot.content)
}

// Mode returns the observed permission bits.
func (snapshot RegularFileSnapshot) Mode() fs.FileMode {
	return snapshot.mode
}

// Identity returns the no-follow entry identity observed by the same stable
// read as Content and Mode.
func (snapshot RegularFileSnapshot) Identity() EntryIdentity {
	return snapshot.identity
}

// TreeRelativePath is one canonical component sequence below a private tree
// root. It names no host root and grants no filesystem authority.
type TreeRelativePath struct {
	components []string
}

// NewTreeRelativePath constructs a path from already separated entry names.
func NewTreeRelativePath(components ...string) (TreeRelativePath, error) {
	if len(components) == 0 {
		return TreeRelativePath{}, fmt.Errorf("tree relative path requires at least one component")
	}
	canonical := make([]string, len(components))
	for index, component := range components {
		if component == "" || component == "." || component == ".." {
			return TreeRelativePath{}, fmt.Errorf("tree path component %d is not canonical", index)
		}
		if strings.Contains(component, "/") || strings.ContainsRune(component, '\x00') {
			return TreeRelativePath{}, fmt.Errorf("tree path component %d contains a separator or NUL", index)
		}
		canonical[index] = component
	}
	return TreeRelativePath{components: canonical}, nil
}

// Validate rejects a zero or non-canonical tree-relative path.
func (path TreeRelativePath) Validate() error {
	_, err := NewTreeRelativePath(path.components...)
	return err
}

// Path returns the canonical slash-separated path below the tree root.
func (path TreeRelativePath) Path() string {
	return strings.Join(path.components, "/")
}

// Depth returns the number of path components below the private tree root.
func (path TreeRelativePath) Depth() int {
	return len(path.components)
}

// RootedTreeSnapshotSink receives one stable rooted directory snapshot in
// depth-first component-lexical order. File content is valid only during
// VisitRegularFile.
type RootedTreeSnapshotSink interface {
	VisitRoot(mode fs.FileMode) error
	VisitDirectory(path TreeRelativePath, mode fs.FileMode) error
	VisitRegularFile(path TreeRelativePath, mode fs.FileMode, size int64, content io.Reader) error
}

// RootedTreeWriter admits tree entries only below one boundary-private stage.
// It is valid only while the preparing callback is running.
type RootedTreeWriter interface {
	SetRootMode(mode fs.FileMode) error
	CreateDirectory(path TreeRelativePath, mode fs.FileMode) error
	WriteFile(path TreeRelativePath, mode fs.FileMode, content io.Reader) error
}

// PreparedRootedTree owns one private stage and its retained-root capability
// until Commit or Abort consumes it.
type PreparedRootedTree interface {
	Commit(ctx context.Context) error
	CommitWithOutcome(ctx context.Context) (CommitOutcome, error)
	Abort(ctx context.Context) error
}
