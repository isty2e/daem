package s3object

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourcearchive "github.com/isty2e/daem/internal/supply/source/archive"
	sourcecache "github.com/isty2e/daem/internal/supply/source/cache"
	"github.com/isty2e/daem/internal/supply/source/directfile"
)

type client interface {
	GetObject(ctx context.Context, input *awss3.GetObjectInput, options ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
}

// Resolver resolves S3 object sources into cached local artifacts.
type Resolver struct {
	state *resolverState
}

type resolverState struct {
	cacheRoot      string
	clients        *clientPool
	artifactLocker sourcecache.Locker
	immutableIndex immutableLookupIndex
	resolveGroup   resolutionGroup

	testBeforeHash func()
}

// NewResolver constructs an S3 resolver rooted at cacheRoot.
func NewResolver(cacheRoot string) (Resolver, error) {
	return newResolverWithClientFactory(cacheRoot, defaultClientFactory)
}

func newResolverWithClientFactory(cacheRoot string, clientFactory clientFactory) (Resolver, error) {
	root := cacheRoot
	if root == "" {
		root = "."
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Resolver{}, fmt.Errorf("resolve s3 source cache root %q: %w", root, err)
	}
	if clientFactory == nil {
		return Resolver{}, fmt.Errorf("s3 client factory is required")
	}

	cleanRoot := filepath.Clean(absoluteRoot)
	return Resolver{
		state: &resolverState{
			cacheRoot:      cleanRoot,
			clients:        newClientPool(clientFactory),
			artifactLocker: sourcecache.NewLocker(filepath.Join(cleanRoot, "locks", "s3-artifact")),
			immutableIndex: newImmutableLookupIndex(cleanRoot),
		},
	}, nil
}

// Resolve downloads and materializes an S3 object source.
func (resolver Resolver) Resolve(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	if ctx == nil {
		return acquisition.Resolution{}, fmt.Errorf("s3 resolver context is required")
	}
	if err := ctx.Err(); err != nil {
		return acquisition.Resolution{}, err
	}
	state, err := resolver.requireState()
	if err != nil {
		return acquisition.Resolution{}, err
	}

	s3Source, ok := sourceSpec.S3()
	if !ok {
		return acquisition.Resolution{}, fmt.Errorf("s3 resolver only supports s3 sources, got %q", sourceSpec.Kind())
	}

	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}

	request := resolveRequest{
		sourceSpec: sourceSpec,
		sourceID:   sourceID,
		s3Source:   s3Source,
		objectURI:  s3Source.ObjectURI(),
		format:     s3Source.Format(),
		options:    options,
	}

	return state.resolveGroup.do(ctx, sourceID, func(ctx context.Context) (acquisition.Resolution, error) {
		return resolver.resolveOnce(ctx, request)
	})
}

type resolveRequest struct {
	sourceSpec source.Source
	sourceID   artifact.SourceID
	s3Source   source.S3Source
	objectURI  source.S3ObjectURI
	format     source.S3ObjectFormat
	options    acquisition.OperationOptions
}

func (resolver Resolver) resolveRemote(ctx context.Context, request resolveRequest) (acquisition.Resolution, error) {
	if err := ctx.Err(); err != nil {
		return acquisition.Resolution{}, err
	}
	state, err := resolver.requireState()
	if err != nil {
		return acquisition.Resolution{}, err
	}

	client, err := state.clients.get(ctx, clientConfigurationFor(request.s3Source))
	if err != nil {
		return acquisition.Resolution{}, err
	}

	input := &awss3.GetObjectInput{
		Bucket: aws.String(request.objectURI.Bucket()),
		Key:    aws.String(request.objectURI.Key()),
	}
	versionID := request.s3Source.VersionID()
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}

	request.options.Emit(acquisition.EventDownload, request.sourceSpec, request.sourceID, "", nil)
	output, err := client.GetObject(ctx, input)
	if err != nil {
		return acquisition.Resolution{}, fmt.Errorf("get s3 object %s: %w", request.objectURI.Canonical(), err)
	}
	if output.Body == nil {
		return acquisition.Resolution{}, fmt.Errorf("get s3 object %s: empty response body", request.objectURI.Canonical())
	}
	defer output.Body.Close()
	if output.ContentLength != nil {
		if request.format == source.S3ObjectFormatFile {
			if err := directfile.CheckKnownSize(*output.ContentLength); err != nil {
				return acquisition.Resolution{}, fmt.Errorf("check s3 direct file size for %s: %w", request.objectURI.Canonical(), err)
			}
		} else {
			if err := sourcearchive.CheckInputSize(*output.ContentLength); err != nil {
				return acquisition.Resolution{}, fmt.Errorf("check s3 archive size for %s: %w", request.objectURI.Canonical(), err)
			}
		}
	}

	resolvedRef := versionID
	if outputVersionID := aws.ToString(output.VersionId); outputVersionID != "" {
		resolvedRef = outputVersionID
	}
	canonicalResolvedRef := artifact.ResolvedRef(resolvedRef)
	if err := source.ValidateResolutionCorrelation(request.sourceSpec, request.sourceID, canonicalResolvedRef); err != nil {
		return acquisition.Resolution{}, err
	}

	contentPath, artifactKind, contentHash, err := resolver.materialize(
		ctx,
		request.sourceSpec,
		request.sourceID,
		canonicalResolvedRef,
		request.format,
		output.Body,
		request.options,
	)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	return resolutionFromMaterialized(
		ctx,
		request.sourceSpec,
		request.sourceID,
		canonicalResolvedRef,
		contentPath,
		artifactKind,
		contentHash,
	)
}

func defaultClientFactory(ctx context.Context, configuration clientConfiguration) (client, error) {
	options := make([]func(*config.LoadOptions) error, 0, 1)
	if region := configuration.region; region != "" {
		options = append(options, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	return awss3.NewFromConfig(cfg), nil
}

func (resolver Resolver) materialize(
	ctx context.Context,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	resolvedRef artifact.ResolvedRef,
	format source.S3ObjectFormat,
	body io.Reader,
	options acquisition.OperationOptions,
) (string, artifact.ArtifactKind, artifact.ContentHash, error) {
	state, err := resolver.requireState()
	if err != nil {
		return "", "", "", err
	}

	artifactParent := resolver.artifactParent(sourceID)
	if err := os.MkdirAll(artifactParent, 0o700); err != nil {
		return "", "", "", fmt.Errorf("create s3 artifact cache directory %q: %w", artifactParent, err)
	}

	tempRoot, err := os.MkdirTemp(artifactParent, ".tmp.*")
	if err != nil {
		return "", "", "", fmt.Errorf("create temporary s3 artifact directory: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(tempRoot)
		}
	}()

	contentPath := filepath.Join(tempRoot, "content")
	if err := materializeBody(ctx, body, contentPath, format); err != nil {
		return "", "", "", err
	}

	if state.testBeforeHash != nil {
		state.testBeforeHash()
	}

	options.Emit(acquisition.EventHash, sourceSpec, sourceID, resolvedRef, nil)
	view, err := access.OpenView(contentPath)
	if err != nil {
		return "", "", "", err
	}
	var contentHash artifact.ContentHash
	if view.Kind() == artifact.ArtifactKindFile {
		contentHash, err = directfile.Hash(ctx, view)
	} else {
		contentHash, err = view.Hash(ctx)
	}
	if err != nil {
		return "", "", "", err
	}
	artifactKind := view.Kind()

	finalRoot := resolver.artifactEntryRoot(sourceID, resolvedRef, contentHash)
	key, err := cacheKeyForS3Artifact(sourceID, resolvedRef, contentHash)
	if err != nil {
		return "", "", "", err
	}
	spec, err := sourcecache.NewEntrySpec(key, "content", contentHash, artifactKind)
	if err != nil {
		return "", "", "", err
	}

	options.Emit(acquisition.EventCacheWait, sourceSpec, sourceID, resolvedRef, nil)
	if err := state.artifactLocker.Do(ctx, key, func() error {
		published, err := sourcecache.PublishPreparedDirectory(
			ctx,
			tempRoot,
			finalRoot,
			spec,
			contentHash,
			artifactKind,
		)
		if err != nil {
			return fmt.Errorf("publish verified s3 artifact cache entry %q: %w", finalRoot, err)
		}

		committed = published
		if published {
			options.Emit(acquisition.EventPublished, sourceSpec, sourceID, resolvedRef, nil)
		} else {
			options.Emit(acquisition.EventCacheHit, sourceSpec, sourceID, resolvedRef, nil)
		}
		return nil
	}); err != nil {
		return "", "", "", err
	}

	return filepath.Join(finalRoot, "content"), artifactKind, contentHash, nil
}

func resolutionFromMaterialized(
	ctx context.Context,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	resolvedRef artifact.ResolvedRef,
	contentPath string,
	kind artifact.ArtifactKind,
	contentHash artifact.ContentHash,
) (acquisition.Resolution, error) {
	view, err := access.OpenView(contentPath)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	identity, err := artifact.NewExactIdentity(sourceID, resolvedRef, kind, contentHash)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	if kind == artifact.ArtifactKindFile {
		contentHash, err := directfile.Hash(ctx, view)
		if err != nil {
			return acquisition.Resolution{}, err
		}
		if contentHash != identity.ContentHash() {
			return acquisition.Resolution{}, fmt.Errorf(
				"artifact content hash %q does not match expected hash %q",
				contentHash,
				identity.ContentHash(),
			)
		}
	} else {
		if err := view.Verify(ctx, identity); err != nil {
			return acquisition.Resolution{}, err
		}
	}
	return acquisition.NewResolution(sourceSpec, identity, view)
}

func materializeBody(ctx context.Context, body io.Reader, contentPath string, format source.S3ObjectFormat) error {
	switch format {
	case source.S3ObjectFormatFile:
		return writeObjectFile(ctx, body, contentPath)
	case source.S3ObjectFormatTar:
		return sourcearchive.ExtractTar(ctx, body, contentPath)
	case source.S3ObjectFormatTarGzip:
		return sourcearchive.ExtractTarGzip(ctx, body, contentPath)
	default:
		return fmt.Errorf("unsupported S3 object format %q", format)
	}
}

func writeObjectFile(ctx context.Context, body io.Reader, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create s3 object parent directory %q: %w", filepath.Dir(path), err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create s3 object artifact %q: %w", path, err)
	}

	if err := directfile.Copy(ctx, file, body); err != nil {
		file.Close()
		return fmt.Errorf("materialize s3 direct file: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close s3 object artifact %q: %w", path, err)
	}

	return nil
}

func cacheKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (resolver Resolver) requireState() (*resolverState, error) {
	if resolver.state == nil || resolver.state.clients == nil {
		return nil, fmt.Errorf("s3 resolver is not initialized")
	}

	return resolver.state, nil
}

func (resolver Resolver) artifactParent(sourceID artifact.SourceID) string {
	if resolver.state == nil {
		return filepath.Join("artifacts", cacheKey(string(sourceID)))
	}

	return filepath.Join(resolver.state.cacheRoot, "artifacts", cacheKey(string(sourceID)))
}

func (resolver Resolver) artifactEntryRoot(sourceID artifact.SourceID, resolvedRef artifact.ResolvedRef, contentHash artifact.ContentHash) string {
	return filepath.Join(resolver.artifactParent(sourceID), cacheKey(string(resolvedRef)+"\n"+string(contentHash)))
}

func cacheKeyForS3Artifact(sourceID artifact.SourceID, resolvedRef artifact.ResolvedRef, contentHash artifact.ContentHash) (sourcecache.Key, error) {
	return sourcecache.NewKey("s3-artifact", string(sourceID), string(resolvedRef), string(contentHash))
}
