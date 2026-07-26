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
)

// EntryIdentity is operation-local evidence returned by a filesystem
// boundary. It grants no mutation authority and has no durable representation.
type EntryIdentity interface {
	Equal(EntryIdentity) bool
	Kind() EntryKind
}

// RegularFileSnapshot is immutable content and mode from one identity-stable,
// no-follow regular-file read.
type RegularFileSnapshot struct {
	content []byte
	mode    fs.FileMode
}

// NewRegularFileSnapshot constructs an immutable file snapshot.
func NewRegularFileSnapshot(content []byte, mode fs.FileMode) RegularFileSnapshot {
	return RegularFileSnapshot{
		content: slices.Clone(content),
		mode:    mode.Perm(),
	}
}

// Content returns an owned copy of the observed file bytes.
func (snapshot RegularFileSnapshot) Content() []byte {
	return slices.Clone(snapshot.content)
}

// Mode returns the observed permission bits.
func (snapshot RegularFileSnapshot) Mode() fs.FileMode {
	return snapshot.mode
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

// RootedTreeSnapshotSink receives one stable rooted directory snapshot in
// depth-first lexical order. File content is valid only during VisitRegularFile.
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
	Abort(ctx context.Context) error
}
