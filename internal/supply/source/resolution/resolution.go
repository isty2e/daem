package resolution

import (
	"context"
	"fmt"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/backend/gitcli"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
	"github.com/isty2e/daem/internal/supply/source/backend/s3object"
)

// Resolver dispatches lockable sources to their concrete backend resolvers.
type Resolver struct {
	local sourceRootResolver
	git   sourceRootResolver
	s3    acquisition.Resolver
}

var _ acquisition.ResolutionSessionProvider = Resolver{}

type sourceRootResolver interface {
	acquisition.Resolver
	acquisition.RootLister
}

// NewResolver creates the backend resolver set used for lock and lock comparison flows.
func NewResolver(paths daempaths.Paths) (Resolver, error) {
	localResolver, err := localfs.NewResolver(paths.ManifestRoot)
	if err != nil {
		return Resolver{}, err
	}

	gitResolver, err := gitcli.NewResolver(paths.SourceCacheDir)
	if err != nil {
		return Resolver{}, err
	}

	s3Resolver, err := s3object.NewResolver(paths.SourceCacheDir)
	if err != nil {
		return Resolver{}, err
	}

	return Resolver{
		local: localResolver,
		git:   gitResolver,
		s3:    s3Resolver,
	}, nil
}

// NewResolutionSession returns a resolver with fresh operation-local backend
// facts while retaining the shared filesystem cache and synchronization roots.
func (resolver Resolver) NewResolutionSession() (acquisition.Resolver, error) {
	return resolver.withResolutionSession(), nil
}

func (resolver Resolver) withResolutionSession() Resolver {
	if gitResolver, ok := resolver.git.(gitcli.Resolver); ok {
		resolver.git = gitResolver.WithRepositorySnapshotSession()
	}
	return resolver
}

// Resolve resolves a source through its registered backend resolver.
func (resolver Resolver) Resolve(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	switch sourceSpec.Kind() {
	case source.SourceKindLocal:
		return resolver.local.Resolve(ctx, sourceSpec, options)
	case source.SourceKindGit:
		return resolver.git.Resolve(ctx, sourceSpec, options)
	case source.SourceKindS3:
		return resolver.s3.Resolve(ctx, sourceSpec, options)
	default:
		return acquisition.Resolution{}, fmt.Errorf("unsupported source kind %q", sourceSpec.Kind())
	}
}

// ListSourceRoot lists direct source-root entries for selector-backed local and Git sources.
func (resolver Resolver) ListSourceRoot(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (source.RootListing, error) {
	switch sourceSpec.Kind() {
	case source.SourceKindLocal:
		return resolver.local.ListSourceRoot(ctx, sourceSpec, options)
	case source.SourceKindGit:
		return resolver.git.ListSourceRoot(ctx, sourceSpec, options)
	case source.SourceKindS3:
		return source.RootListing{}, fmt.Errorf("S3 skill groups are unsupported; S3 prefix directory sources are unsupported")
	default:
		return source.RootListing{}, fmt.Errorf("unsupported source kind %q", sourceSpec.Kind())
	}
}
