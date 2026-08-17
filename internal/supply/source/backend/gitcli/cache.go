package gitcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourcecache "github.com/isty2e/daem/internal/supply/source/cache"
)

type repositoryResolution struct {
	repository cachedRepository
	commit     string
}

func (resolver Resolver) resolveRepositoryCommit(
	ctx context.Context,
	gitSource source.GitSource,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (repositoryResolution, error) {
	if resolver.session != nil {
		resolution, err := resolver.session.resolve(ctx, repositoryKey(gitSource), func() (repositoryResolution, error) {
			return resolver.resolveRepositorySnapshot(ctx, gitSource, sourceSpec, sourceID, options)
		})
		if err != nil {
			return repositoryResolution{}, err
		}
		return resolution, nil
	}

	resolution, err := resolver.resolveRepositorySnapshot(ctx, gitSource, sourceSpec, sourceID, options)
	if err != nil {
		return repositoryResolution{}, err
	}
	return resolution, nil
}

func (resolver Resolver) resolveRepositorySnapshot(
	ctx context.Context,
	gitSource source.GitSource,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (repositoryResolution, error) {
	state, err := resolver.requireState()
	if err != nil {
		return repositoryResolution{}, err
	}

	locator := gitSource.Locator().String()
	key, err := cacheKeyForGitRepo(locator)
	if err != nil {
		return repositoryResolution{}, err
	}

	var resolution repositoryResolution
	cacheRoot, err := resolver.captureCacheRoot(ctx)
	if err != nil {
		return repositoryResolution{}, err
	}
	options.Emit(acquisition.EventCacheWait, sourceSpec, sourceID, "", nil)
	lockErr := state.repoLocker.DoRooted(ctx, cacheRoot, key, func() error {
		repository, err := resolver.ensureRepository(
			ctx,
			cacheRoot,
			gitSource,
			sourceSpec,
			sourceID,
			options,
		)
		if err != nil {
			return err
		}

		commit, err := resolver.resolveCommit(ctx, repository, gitSource.Ref(), sourceSpec, sourceID, options)
		if err != nil {
			return err
		}

		resolution = repositoryResolution{
			repository: repository,
			commit:     commit,
		}
		return nil
	})
	closeErr := cacheRoot.Close()
	if lockErr != nil || closeErr != nil {
		return repositoryResolution{}, errors.Join(lockErr, closeErr)
	}

	return resolution, nil
}

func (resolver Resolver) ensureRepository(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	gitSource source.GitSource,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (repository cachedRepository, returnErr error) {
	locator := gitSource.Locator().String()
	if localPath, ok := gitSource.Locator().LocalPath(); ok {
		info, err := os.Stat(localPath)
		if err == nil && !info.IsDir() {
			return cachedRepository{}, fmt.Errorf("git locator local input must be a repository directory; bundle files are unsupported")
		}
		if err != nil && !os.IsNotExist(err) {
			return cachedRepository{}, fmt.Errorf("inspect local git locator: %w", err)
		}
	}
	format, err := resolver.observeObjectFormat(ctx, cacheRoot, gitSource)
	if err != nil {
		return cachedRepository{}, err
	}
	repository = newCachedRepository(resolver, gitSource.Locator(), format)
	created, err := resolver.ensureRepositoryCacheEntry(ctx, cacheRoot, repository)
	if err != nil {
		return cachedRepository{}, err
	}

	handle, err := resolver.openRepository(ctx, repository)
	if err != nil {
		return cachedRepository{}, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, handle.Close())
	}()
	if created {
		if err := handle.initialize(ctx); err != nil {
			return cachedRepository{}, err
		}
	} else {
		if err := handle.verifyBare(ctx); err != nil {
			return cachedRepository{}, err
		}
		if err := handle.verifyOrigin(ctx); err != nil {
			return cachedRepository{}, err
		}
		if err := handle.verifyLocalConfiguration(ctx); err != nil {
			return cachedRepository{}, err
		}
		options.Emit(acquisition.EventCacheHit, sourceSpec, sourceID, "", nil)
	}
	options.Emit(acquisition.EventFetch, sourceSpec, sourceID, "", nil)
	if err := handle.runGit(ctx, refreshArgs()...); err != nil {
		return cachedRepository{}, fmt.Errorf("fetch git source %q: %w", locator, err)
	}

	return repository, nil
}

func (resolver Resolver) repositoryPath(url string) string {
	return resolver.repositoryCachePath(url, gitObjectFormatSHA1)
}

func (resolver Resolver) repositoryCachePath(locator string, format gitObjectFormat) string {
	return filepath.Join(resolver.cacheRoot(), "repos", repositoryCacheDirectoryName(locator, format))
}

func (resolver Resolver) artifactRoot(url string, commit string, gitPath string) string {
	return filepath.Join(resolver.artifactEntryRoot(url, commit, gitPath), "content")
}

func (resolver Resolver) artifactEntryRoot(url string, commit string, gitPath string) string {
	return filepath.Join(resolver.cacheRoot(), "artifacts", cacheKey(url), commit, cacheKey(gitPath))
}

func (resolver Resolver) artifactEntryRelativeRoot(url string, commit string, gitPath string) string {
	return path.Join("artifacts", cacheKey(url), commit, cacheKey(gitPath))
}

func (resolver Resolver) cacheRoot() string {
	if resolver.state == nil {
		return ""
	}

	return resolver.state.cacheRoot
}

func cacheKeyForGitRepo(url string) (sourcecache.Key, error) {
	return sourcecache.NewKey("git-repo", url)
}

func cacheKeyForGitArtifact(url string, commit string, gitPath string) (sourcecache.Key, error) {
	return sourcecache.NewKey("git-artifact", url, commit, gitPath)
}

func cacheKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func repositoryCacheDirectoryName(locator string, format gitObjectFormat) string {
	if format == gitObjectFormatSHA1 {
		return cacheKey(locator)
	}
	return cacheKey(locator + "\n" + string(format))
}
