package localfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/directfile"
)

// Resolver resolves local filesystem sources relative to a manifest root.
type Resolver struct {
	root string
}

type sourceUnavailableError struct {
	path  string
	cause error
}

func (err *sourceUnavailableError) Error() string {
	return fmt.Sprintf("source path %q does not exist: %v", err.path, err.cause)
}

func (err *sourceUnavailableError) Unwrap() error { return err.cause }

// IsSourceUnavailable reports whether resolution failed because the requested
// local source root was absent before traversal began.
func IsSourceUnavailable(err error) bool {
	var unavailable *sourceUnavailableError
	return errors.As(err, &unavailable)
}

// NewResolver constructs a local filesystem resolver rooted at root.
func NewResolver(root string) (Resolver, error) {
	rootPath := root
	if rootPath == "" {
		rootPath = "."
	}

	absoluteRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return Resolver{}, fmt.Errorf("resolve local source root %q: %w", rootPath, err)
	}

	return Resolver{root: filepath.Clean(absoluteRoot)}, nil
}

// Resolve resolves and hashes a local source without copying it into vendor storage.
func (resolver Resolver) Resolve(ctx context.Context, sourceSpec source.Source) (acquisition.Resolution, error) {
	return resolver.ResolveWithOptions(ctx, sourceSpec, acquisition.OperationOptions{})
}

// ResolveWithOptions resolves and hashes a local source with source-owned operation options.
func (resolver Resolver) ResolveWithOptions(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	if ctx == nil {
		return acquisition.Resolution{}, fmt.Errorf("local resolver context is required")
	}
	if err := ctx.Err(); err != nil {
		return acquisition.Resolution{}, err
	}
	localSource, ok := sourceSpec.Local()
	if !ok {
		return acquisition.Resolution{}, fmt.Errorf("local resolver only supports local sources, got %q", sourceSpec.Kind())
	}

	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	contentPath := resolver.resolvePath(localSource.Path())
	options.Emit(acquisition.EventHash, sourceSpec, sourceID, "", nil)
	view, err := access.OpenView(contentPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return acquisition.Resolution{}, &sourceUnavailableError{path: contentPath, cause: err}
		}
		return acquisition.Resolution{}, fmt.Errorf("open local source %q: %w", contentPath, err)
	}
	var contentHash artifact.ContentHash
	if view.Kind() == artifact.ArtifactKindFile {
		contentHash, err = directfile.Hash(ctx, view)
	} else {
		contentHash, err = view.Hash(ctx)
	}
	if err != nil {
		return acquisition.Resolution{}, err
	}
	identity, err := artifact.NewExactIdentity(sourceID, "", view.Kind(), contentHash)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	return acquisition.NewResolution(sourceSpec, identity, view)
}

// LocalInputAuthorityPath projects the path used only by outer workspace
// mutation coordination for a validated local source. It is not artifact
// identity or materialized-content access.
func (resolver Resolver) LocalInputAuthorityPath(sourceSpec source.Source) (string, error) {
	return resolver.contentPath(sourceSpec)
}

func (resolver Resolver) contentPath(sourceSpec source.Source) (string, error) {
	localSource, ok := sourceSpec.Local()
	if !ok {
		return "", fmt.Errorf("local resolver only supports local sources, got %q", sourceSpec.Kind())
	}
	if _, err := source.SourceIDFor(sourceSpec); err != nil {
		return "", err
	}
	return resolver.resolvePath(localSource.Path()), nil
}

// ListSourceRoot lists direct child directories of a local source root without hashing contents.
func (resolver Resolver) ListSourceRoot(ctx context.Context, sourceSpec source.Source) (source.RootListing, error) {
	return resolver.ListSourceRootWithOptions(ctx, sourceSpec, acquisition.OperationOptions{})
}

// ListSourceRootWithOptions lists direct child directories with source-owned operation options.
func (resolver Resolver) ListSourceRootWithOptions(
	ctx context.Context,
	sourceSpec source.Source,
	_ acquisition.OperationOptions,
) (source.RootListing, error) {
	if ctx == nil {
		return source.RootListing{}, fmt.Errorf("local root listing context is required")
	}
	if err := ctx.Err(); err != nil {
		return source.RootListing{}, err
	}

	contentPath, err := resolver.contentPath(sourceSpec)
	if err != nil {
		return source.RootListing{}, err
	}
	view, err := access.OpenView(contentPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return source.RootListing{}, &sourceUnavailableError{path: contentPath, cause: err}
		}
		return source.RootListing{}, fmt.Errorf("open local source root %q: %w", contentPath, err)
	}
	if view.Kind() == artifact.ArtifactKindFile {
		return source.NewRootListing(sourceSpec, "", artifact.ArtifactKindFile, nil)
	}

	entries, err := view.ReadDirectory(ctx, ".")
	if err != nil {
		return source.RootListing{}, err
	}
	childNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind() != access.EntryKindDirectory {
			continue
		}
		childNames = append(childNames, entry.Name())
	}

	if err := ctx.Err(); err != nil {
		return source.RootListing{}, err
	}

	return source.NewRootListing(sourceSpec, "", artifact.ArtifactKindDirectory, childNames)
}

func (resolver Resolver) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	return filepath.Clean(filepath.Join(resolver.root, path))
}
