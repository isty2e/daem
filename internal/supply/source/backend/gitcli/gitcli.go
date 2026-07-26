package gitcli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourcecache "github.com/isty2e/daem/internal/supply/source/cache"
)

const gitExecutable = "git"

// Resolver resolves Git sources through the user's system git.
type Resolver struct {
	state   *resolverState
	session *repositorySnapshotSession
}

// WithRepositorySnapshotSession returns a resolver that reuses immutable
// repository snapshots only for the lifetime of the returned resolver value.
func (resolver Resolver) WithRepositorySnapshotSession() Resolver {
	if resolver.session == nil {
		resolver.session = newRepositorySnapshotSession()
	}
	return resolver
}

type resolverState struct {
	cacheRoot      string
	repoLocker     sourcecache.Locker
	artifactLocker sourcecache.Locker

	testAfterArchiveExtract func()
}

// NewResolver constructs a Git CLI resolver rooted at cacheRoot.
func NewResolver(cacheRoot string) (Resolver, error) {
	root := cacheRoot
	if root == "" {
		root = "."
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Resolver{}, fmt.Errorf("resolve git source cache root %q: %w", root, err)
	}

	cleanRoot := filepath.Clean(absoluteRoot)
	return Resolver{
		state: &resolverState{
			cacheRoot:      cleanRoot,
			repoLocker:     sourcecache.NewLocker(filepath.Join(cleanRoot, "locks", "git-repo")),
			artifactLocker: sourcecache.NewLocker(filepath.Join(cleanRoot, "locks", "git-artifact")),
		},
	}, nil
}

// Resolve resolves a Git source to an exported artifact path and immutable commit.
func (resolver Resolver) Resolve(ctx context.Context, sourceSpec source.Source) (acquisition.Resolution, error) {
	return resolver.ResolveWithOptions(ctx, sourceSpec, acquisition.OperationOptions{})
}

// ResolveWithOptions resolves a Git source with source-owned operation options.
func (resolver Resolver) ResolveWithOptions(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	if ctx == nil {
		return acquisition.Resolution{}, fmt.Errorf("git resolver context is required")
	}
	if err := ctx.Err(); err != nil {
		return acquisition.Resolution{}, err
	}
	if _, err := resolver.requireState(); err != nil {
		return acquisition.Resolution{}, err
	}

	gitSource, ok := sourceSpec.Git()
	if !ok {
		return acquisition.Resolution{}, fmt.Errorf("git resolver only supports git sources, got %q", sourceSpec.Kind())
	}

	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}

	gitPath := gitSource.RepositoryPath().String()

	repoPath, commit, err := resolver.resolveRepositoryCommit(ctx, gitSource, sourceSpec, sourceID, options)
	if err != nil {
		return acquisition.Resolution{}, err
	}

	contentPath, err := resolver.ensureArtifact(ctx, gitSource.Locator().String(), repoPath, commit, gitPath, sourceSpec, sourceID, options)
	if err != nil {
		return acquisition.Resolution{}, err
	}

	options.Emit(acquisition.EventHash, sourceSpec, sourceID, artifact.ResolvedRef(commit), nil)
	view, err := access.OpenView(contentPath)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	contentHash, err := view.Hash(ctx)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	identity, err := artifact.NewExactIdentity(sourceID, artifact.ResolvedRef(commit), view.Kind(), contentHash)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	return acquisition.NewResolution(sourceSpec, identity, view)
}

func (resolver Resolver) requireState() (*resolverState, error) {
	if resolver.state == nil {
		return nil, fmt.Errorf("git resolver is not initialized")
	}

	return resolver.state, nil
}
