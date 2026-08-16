package access

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/supply/artifact"
)

// ErrRequiredRootRegularFile reports that a directory artifact did not contain
// one required regular file at its root during the identity traversal.
var ErrRequiredRootRegularFile = errors.New("required root regular file is unavailable")

// ErrUnsupportedSymlink reports a no-follow traversal encountering a symbolic link.
var ErrUnsupportedSymlink = errors.New("symbolic link is unsupported")

type unsupportedSymlinkError struct {
	path string
}

func (err *unsupportedSymlinkError) Error() string {
	return fmt.Sprintf("artifact access entry %q is a symbolic link; symlinks are not supported", err.path)
}

func (err *unsupportedSymlinkError) Unwrap() error {
	return ErrUnsupportedSymlink
}

// UnsupportedSymlinkPath reports the relative artifact path of an unsupported
// symbolic link encountered during no-follow traversal.
func UnsupportedSymlinkPath(err error) (string, bool) {
	var symlink *unsupportedSymlinkError
	if !errors.As(err, &symlink) {
		return "", false
	}
	return symlink.path, true
}

// EntryKind classifies one no-follow directory entry.
type EntryKind string

const (
	EntryKindFile        EntryKind = "file"
	EntryKindDirectory   EntryKind = "directory"
	EntryKindSymlink     EntryKind = "symlink"
	EntryKindUnsupported EntryKind = "unsupported"
)

// Entry is immutable no-follow metadata for one direct directory entry.
type Entry struct {
	name string
	kind EntryKind
	mode fs.FileMode
}

// Name returns the exact directory entry name reported by the filesystem.
func (entry Entry) Name() string { return entry.name }

// Kind returns the no-follow structural entry kind.
func (entry Entry) Kind() EntryKind { return entry.kind }

// Mode returns the recorded permission bits.
func (entry Entry) Mode() fs.FileMode { return entry.mode }

// FileContent is one bounded regular-file read.
type FileContent struct {
	content []byte
	mode    fs.FileMode
}

// Bytes returns an independent copy of the file bytes.
func (content FileContent) Bytes() []byte { return append([]byte(nil), content.content...) }

// Mode returns the regular file permission bits observed for this read.
func (content FileContent) Mode() fs.FileMode { return content.mode }

// TreeSink receives an artifact into caller-owned unpublished staging.
// Implementations must not make the staged tree host-visible before
// CopyVerified returns successfully.
type TreeSink interface {
	BeginDirectory(relativePath string, mode fs.FileMode) error
	OpenFile(relativePath string, mode fs.FileMode, size int64) (io.WriteCloser, error)
	EndDirectory(relativePath string, mode fs.FileMode) error
}

// View is a copyable, non-owning capability for one local or verified-cache
// artifact root. It contains no descriptor, mutable cursor, or verified bit.
type View struct {
	root string
	kind artifact.ArtifactKind
}

// TraversalLimit bounds one complete artifact hash traversal. The entry count
// includes the artifact root.
type TraversalLimit struct {
	maxEntries uint64
	maxBytes   int64
}

// TraversalMeasurement is exact bounded work observed during one complete
// artifact traversal. Entries exclude the selected root.
type TraversalMeasurement struct {
	descendantEntries int
	regularFileBytes  int64
}

// DescendantEntries returns the complete descendant cardinality.
func (measurement TraversalMeasurement) DescendantEntries() int {
	return measurement.descendantEntries
}

// RegularFileBytes returns cumulative regular-file content bytes.
func (measurement TraversalMeasurement) RegularFileBytes() int64 {
	return measurement.regularFileBytes
}

// NewTraversalLimit constructs a positive entry and regular-file byte budget.
func NewTraversalLimit(maxEntries uint64, maxBytes int64) (TraversalLimit, error) {
	limit := TraversalLimit{maxEntries: maxEntries, maxBytes: maxBytes}
	if err := limit.validate(); err != nil {
		return TraversalLimit{}, err
	}
	return limit, nil
}

// OpenView validates and captures a canonical root locator without retaining
// an open descriptor. Every later operation reopens and revalidates the root.
func OpenView(root string) (View, error) {
	canonicalRoot, err := canonicalizeRoot(root)
	if err != nil {
		return View{}, err
	}
	kind, err := inspectNative(canonicalRoot)
	if err != nil {
		return View{}, err
	}
	view := View{root: canonicalRoot, kind: kind}
	if err := view.validate(); err != nil {
		return View{}, err
	}
	return view, nil
}

// OpenNoFollowView captures an absolute root without resolving any symbolic
// link component. It is intended for security-sensitive host observations
// where a link in an ancestor is itself indeterminate evidence.
func OpenNoFollowView(root string) (View, error) {
	absoluteRoot, err := absoluteRoot(root)
	if err != nil {
		return View{}, err
	}
	kind, err := inspectNative(absoluteRoot)
	if err != nil {
		return View{}, err
	}
	view := View{root: absoluteRoot, kind: kind}
	if err := view.validate(); err != nil {
		return View{}, err
	}
	return view, nil
}

// Kind returns the structural artifact kind admitted for this view.
func (view View) Kind() artifact.ArtifactKind { return view.kind }

// ReadDirectory returns sorted no-follow metadata for one directory.
func (view View) ReadDirectory(ctx context.Context, relativePath string) ([]Entry, error) {
	if err := view.validateOperation(ctx); err != nil {
		return nil, err
	}
	if err := validateRelativePath(relativePath); err != nil {
		return nil, err
	}
	return readDirectoryNative(ctx, view.root, view.kind, relativePath)
}

// VisitDirectory streams no-follow metadata for one directory without
// materializing its complete entry set. Visit order is filesystem-defined;
// callers that publish names must canonicalize their final ordering.
func (view View) VisitDirectory(
	ctx context.Context,
	relativePath string,
	visit func(Entry) error,
) error {
	if err := view.validateOperation(ctx); err != nil {
		return err
	}
	if err := validateRelativePath(relativePath); err != nil {
		return err
	}
	if visit == nil {
		return fmt.Errorf("artifact access directory visitor is required")
	}
	return visitDirectoryNative(ctx, view.root, view.kind, relativePath, visit)
}

// ReadFile reads one regular file up to maxBytes without following links.
// It does not claim whole-artifact identity; identity-sensitive consumers use
// Verify or CopyVerified over the complete artifact.
func (view View) ReadFile(
	ctx context.Context,
	relativePath string,
	maxBytes int64,
) (FileContent, error) {
	if err := view.validateOperation(ctx); err != nil {
		return FileContent{}, err
	}
	if err := validateRelativePath(relativePath); err != nil {
		return FileContent{}, err
	}
	if maxBytes <= 0 {
		return FileContent{}, fmt.Errorf("artifact access read limit must be positive")
	}
	content, mode, err := readFileNative(ctx, view.root, view.kind, relativePath, maxBytes)
	if err != nil {
		return FileContent{}, err
	}
	return FileContent{content: append([]byte(nil), content...), mode: mode}, nil
}

// ReadRootFileVerified reads a file artifact once and verifies that the exact
// bytes returned to the caller match the expected identity.
func (view View) ReadRootFileVerified(
	ctx context.Context,
	expected artifact.ExactIdentity,
	maxBytes int64,
) (FileContent, error) {
	if err := expected.Validate(); err != nil {
		return FileContent{}, fmt.Errorf("read artifact identity: %w", err)
	}
	if view.kind != artifact.ArtifactKindFile || expected.Kind() != artifact.ArtifactKindFile {
		return FileContent{}, fmt.Errorf("verified root file read requires a file artifact")
	}
	content, err := view.ReadFile(ctx, ".", maxBytes)
	if err != nil {
		return FileContent{}, err
	}
	contentHash := artifact.HashFileContentWithExecutable(
		content.content,
		content.mode.Perm()&0o111 != 0,
	)
	if contentHash != expected.ContentHash() {
		return FileContent{}, fmt.Errorf(
			"read artifact content hash %q does not match expected hash %q",
			contentHash,
			expected.ContentHash(),
		)
	}
	return content, nil
}

// Hash computes hash-v1 from bytes consumed through a no-follow traversal.
func (view View) Hash(ctx context.Context) (artifact.ContentHash, error) {
	if err := view.validateOperation(ctx); err != nil {
		return "", err
	}
	return walkNative(ctx, view.root, view.kind, nil, nil)
}

// HashDirectoryRequiringRootFile hashes one directory while proving that name
// was a regular root entry in the same descriptor-bound traversal. Missing or
// non-regular required files fail before descendant hashing.
func (view View) HashDirectoryRequiringRootFile(
	ctx context.Context,
	name string,
	structureLimit TreeStructureLimit,
) (artifact.ContentHash, error) {
	if err := view.validateOperation(ctx); err != nil {
		return "", err
	}
	if view.kind != artifact.ArtifactKindDirectory {
		return "", fmt.Errorf("required root file validation needs a directory artifact")
	}
	if strings.TrimSpace(name) != name || name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("required root file name %q must be one canonical entry name", name)
	}
	if err := structureLimit.validate(); err != nil {
		return "", fmt.Errorf("required root file tree structure limit: %w", err)
	}
	observer := &requiredRootRegularFileSink{name: name}
	budget := traversalBudget{
		structureLimit:          &structureLimit,
		requiredRootRegularFile: name,
	}
	contentHash, err := walkNative(ctx, view.root, view.kind, observer, &budget)
	if err != nil {
		return "", err
	}
	if !observer.found {
		return "", fmt.Errorf("%w: %q", ErrRequiredRootRegularFile, name)
	}
	return contentHash, nil
}

// HashWithLimit computes hash-v1 and exact completed work while refusing a
// traversal whose entry count or cumulative regular-file bytes exceed the
// caller-selected budget.
func (view View) HashWithLimit(
	ctx context.Context,
	limit TraversalLimit,
) (artifact.ContentHash, TraversalMeasurement, error) {
	if err := view.validateOperation(ctx); err != nil {
		return "", TraversalMeasurement{}, err
	}
	if err := limit.validate(); err != nil {
		return "", TraversalMeasurement{}, err
	}
	budget := traversalBudget{limit: limit}
	contentHash, err := walkNative(ctx, view.root, view.kind, nil, &budget)
	if err != nil {
		return "", TraversalMeasurement{}, err
	}
	return contentHash, traversalMeasurement(budget), nil
}

// HashDirectoryWithLimits computes hash-v1 and exact work while enforcing
// both traversal-resource and directory-structure limits.
func (view View) HashDirectoryWithLimits(
	ctx context.Context,
	traversalLimit TraversalLimit,
	structureLimit TreeStructureLimit,
) (artifact.ContentHash, TraversalMeasurement, error) {
	if view.kind != artifact.ArtifactKindDirectory {
		return "", TraversalMeasurement{}, fmt.Errorf("bounded directory hash requires a directory artifact")
	}
	if err := view.validateOperation(ctx); err != nil {
		return "", TraversalMeasurement{}, err
	}
	if err := traversalLimit.validate(); err != nil {
		return "", TraversalMeasurement{}, err
	}
	if err := structureLimit.validate(); err != nil {
		return "", TraversalMeasurement{}, err
	}
	budget := traversalBudget{limit: traversalLimit, structureLimit: &structureLimit}
	contentHash, err := walkNative(ctx, view.root, view.kind, nil, &budget)
	if err != nil {
		return "", TraversalMeasurement{}, err
	}
	return contentHash, traversalMeasurement(budget), nil
}

// MeasureVerifiedDirectory verifies one exact directory identity while
// returning the bounded work consumed by the same descriptor-bound traversal.
func (view View) MeasureVerifiedDirectory(
	ctx context.Context,
	expected artifact.ExactIdentity,
	limit TraversalLimit,
	structureLimit TreeStructureLimit,
) (TraversalMeasurement, error) {
	if err := expected.Validate(); err != nil {
		return TraversalMeasurement{}, fmt.Errorf("measure artifact identity: %w", err)
	}
	if view.kind != artifact.ArtifactKindDirectory || expected.Kind() != artifact.ArtifactKindDirectory {
		return TraversalMeasurement{}, fmt.Errorf("measured artifact must be a directory")
	}
	contentHash, measurement, err := view.HashDirectoryWithLimits(ctx, limit, structureLimit)
	if err != nil {
		return TraversalMeasurement{}, err
	}
	if contentHash != expected.ContentHash() {
		return TraversalMeasurement{}, fmt.Errorf(
			"measured artifact content hash %q does not match expected hash %q",
			contentHash,
			expected.ContentHash(),
		)
	}
	return measurement, nil
}

func traversalMeasurement(budget traversalBudget) TraversalMeasurement {
	return TraversalMeasurement{
		descendantEntries: budget.structureEntries,
		regularFileBytes:  budget.bytes,
	}
}

// Verify checks that the bytes consumed now match expected exactly.
func (view View) Verify(ctx context.Context, expected artifact.ExactIdentity) error {
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("verify artifact identity: %w", err)
	}
	if view.kind != expected.Kind() {
		return fmt.Errorf("artifact access kind %q does not match expected kind %q", view.kind, expected.Kind())
	}
	contentHash, err := view.Hash(ctx)
	if err != nil {
		return err
	}
	if contentHash != expected.ContentHash() {
		return fmt.Errorf("artifact content hash %q does not match expected hash %q", contentHash, expected.ContentHash())
	}
	return nil
}

// CopyVerified streams the artifact once into caller-owned unpublished
// staging while hashing the exact emitted bytes. It verifies only the
// structural kind and content hash; source correlation remains source-owned.
func (view View) CopyVerified(
	ctx context.Context,
	expected artifact.ExactIdentity,
	sink TreeSink,
) error {
	return view.copyVerified(ctx, expected, sink, nil)
}

// CopyVerifiedWithLimits streams and verifies one artifact while enforcing
// caller-owned traversal and directory-structure ceilings.
func (view View) CopyVerifiedWithLimits(
	ctx context.Context,
	expected artifact.ExactIdentity,
	sink TreeSink,
	traversalLimit TraversalLimit,
	structureLimit TreeStructureLimit,
) error {
	if err := traversalLimit.validate(); err != nil {
		return err
	}
	if err := structureLimit.validate(); err != nil {
		return err
	}
	budget := traversalBudget{limit: traversalLimit, structureLimit: &structureLimit}
	return view.copyVerified(ctx, expected, sink, &budget)
}

func (view View) copyVerified(
	ctx context.Context,
	expected artifact.ExactIdentity,
	sink TreeSink,
	budget *traversalBudget,
) error {
	if sink == nil {
		return fmt.Errorf("artifact access tree sink is required")
	}
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("copy artifact identity: %w", err)
	}
	if view.kind != expected.Kind() {
		return fmt.Errorf("artifact access kind %q does not match expected kind %q", view.kind, expected.Kind())
	}
	if err := view.validateOperation(ctx); err != nil {
		return err
	}
	contentHash, err := walkNative(ctx, view.root, view.kind, sink, budget)
	if err != nil {
		return err
	}
	if contentHash != expected.ContentHash() {
		return fmt.Errorf(
			"copied artifact content hash %q does not match expected hash %q",
			contentHash,
			expected.ContentHash(),
		)
	}
	return nil
}

func (view View) validate() error {
	if view.root == "" {
		return fmt.Errorf("artifact access root is required")
	}
	switch view.kind {
	case artifact.ArtifactKindFile, artifact.ArtifactKindDirectory:
		return nil
	default:
		return fmt.Errorf("artifact access kind %q is unsupported", view.kind)
	}
}

func (view View) validateOperation(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("artifact access context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return view.validate()
}

func canonicalizeRoot(root string) (string, error) {
	absolute, err := absoluteRoot(root)
	if err != nil {
		return "", err
	}
	if absolute == string(filepath.Separator) {
		return absolute, nil
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve artifact access parent %q: %w", filepath.Dir(absolute), err)
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}

func absoluteRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("artifact access root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve artifact access root %q: %w", root, err)
	}
	return filepath.Clean(absolute), nil
}

func (limit TraversalLimit) validate() error {
	if limit.maxEntries == 0 {
		return fmt.Errorf("artifact traversal entry limit must be positive")
	}
	if limit.maxBytes <= 0 {
		return fmt.Errorf("artifact traversal byte limit must be positive")
	}
	return nil
}

type traversalBudget struct {
	limit                   TraversalLimit
	entries                 uint64
	bytes                   int64
	structureLimit          *TreeStructureLimit
	structureEntries        int
	requiredRootRegularFile string
}

type requiredRootRegularFileSink struct {
	name  string
	found bool
}

func (sink *requiredRootRegularFileSink) BeginDirectory(relativePath string, _ fs.FileMode) error {
	if relativePath == sink.name {
		return fmt.Errorf("%w: %q is a directory", ErrRequiredRootRegularFile, sink.name)
	}
	return nil
}

func (sink *requiredRootRegularFileSink) OpenFile(
	relativePath string,
	_ fs.FileMode,
	_ int64,
) (io.WriteCloser, error) {
	if relativePath == sink.name {
		sink.found = true
	}
	return discardWriteCloser{}, nil
}

func (*requiredRootRegularFileSink) EndDirectory(string, fs.FileMode) error { return nil }

type discardWriteCloser struct{}

func (discardWriteCloser) Write(content []byte) (int, error) { return len(content), nil }

func (discardWriteCloser) Close() error { return nil }

func (budget *traversalBudget) consumeRoot(size int64) error {
	if budget == nil {
		return nil
	}
	return budget.consumeTraversal(".", size)
}

func (budget *traversalBudget) consumeEntry(
	relativePath string,
	size int64,
	directory bool,
	parentDepth int,
) error {
	if budget == nil {
		return nil
	}
	if err := budget.consumeTraversal(relativePath, size); err != nil {
		return err
	}
	if budget.structureLimit == nil {
		budget.structureEntries++
		return nil
	}
	depth := parentDepth
	if directory {
		depth++
	}
	if depth > budget.structureLimit.maximumDepth {
		return fmt.Errorf(
			"artifact tree exceeds maximum depth %d at %q",
			budget.structureLimit.maximumDepth,
			relativePath,
		)
	}
	if budget.structureEntries >= budget.structureLimit.maximumEntries {
		return fmt.Errorf(
			"artifact tree exceeds %d entries at %q",
			budget.structureLimit.maximumEntries,
			relativePath,
		)
	}
	budget.structureEntries++
	return nil
}

func (budget *traversalBudget) structureEntriesRemaining() (int, bool) {
	if budget == nil || budget.structureLimit == nil {
		return 0, false
	}
	return budget.structureLimit.maximumEntries - budget.structureEntries, true
}

func (budget *traversalBudget) consumeTraversal(relativePath string, size int64) error {
	if size < 0 {
		return fmt.Errorf("artifact access path %q reports a negative size", relativePath)
	}
	if budget.limit.maxEntries == 0 && budget.limit.maxBytes == 0 {
		return nil
	}
	if budget.entries >= budget.limit.maxEntries {
		return fmt.Errorf(
			"artifact traversal exceeds entry limit %d at %q",
			budget.limit.maxEntries,
			relativePath,
		)
	}
	if size > budget.limit.maxBytes-budget.bytes {
		observed := int64(math.MaxInt64)
		if size <= math.MaxInt64-budget.bytes {
			observed = budget.bytes + size
		}
		return newLimitError(
			"traversal",
			relativePath,
			budget.limit.maxBytes,
			observed,
		)
	}
	budget.entries++
	budget.bytes += size
	return nil
}

func validateRelativePath(relativePath string) error {
	if relativePath == "." {
		return nil
	}
	if !fs.ValidPath(relativePath) || strings.ContainsRune(relativePath, '\\') {
		return fmt.Errorf("artifact access path %q is not canonical", relativePath)
	}
	if strings.IndexFunc(relativePath, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("artifact access path %q contains an unsafe control character", relativePath)
	}
	return nil
}
