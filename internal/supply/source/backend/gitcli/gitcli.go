package gitcli

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

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

	objectFormatMu                   sync.Mutex
	objectFormatProbed               bool
	explicitObjectFormat             bool
	testAfterArchiveExtract          func()
	testAfterArtifactEnsure          func()
	testBeforeRepositoryCommand      func()
	testRemoteRefAdvertisementBudget *remoteRefAdvertisementBudget
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

	cleanRoot, err := sourcecache.CanonicalRootPath(filepath.Clean(absoluteRoot))
	if err != nil {
		return Resolver{}, fmt.Errorf("resolve physical git source cache root %q: %w", root, err)
	}
	return Resolver{
		state: &resolverState{
			cacheRoot:      cleanRoot,
			repoLocker:     sourcecache.NewLocker(filepath.Join(cleanRoot, "locks", "git-repo")),
			artifactLocker: sourcecache.NewLocker(filepath.Join(cleanRoot, "locks", "git-artifact")),
		},
	}, nil
}

// Resolve resolves a Git source to an exported artifact path and immutable commit.
func (resolver Resolver) Resolve(
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

	snapshot, err := resolver.resolveRepositoryCommit(ctx, gitSource, sourceSpec, sourceID, options)
	if err != nil {
		return acquisition.Resolution{}, err
	}

	contentPath, contentHash, contentKind, err := resolver.ensureArtifact(
		ctx,
		snapshot.repository,
		snapshot.commit,
		gitPath,
		sourceSpec,
		sourceID,
		options,
	)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	if resolver.state != nil && resolver.state.testAfterArtifactEnsure != nil {
		resolver.state.testAfterArtifactEnsure()
	}

	options.Emit(acquisition.EventHash, sourceSpec, sourceID, artifact.ResolvedRef(snapshot.commit), nil)
	view, err := access.OpenNoFollowView(contentPath)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	identity, err := artifact.NewExactIdentity(
		sourceID,
		artifact.ResolvedRef(snapshot.commit),
		contentKind,
		contentHash,
	)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	if err := view.Verify(ctx, identity); err != nil {
		return acquisition.Resolution{}, fmt.Errorf("verify published Git artifact view: %w", err)
	}
	return acquisition.NewResolution(sourceSpec, identity, view)
}

func (resolver Resolver) requireState() (*resolverState, error) {
	if resolver.state == nil {
		return nil, fmt.Errorf("git resolver is not initialized")
	}

	return resolver.state, nil
}
