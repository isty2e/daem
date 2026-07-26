package gitcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourcecache "github.com/isty2e/daem/internal/supply/source/cache"
)

type repositoryResolution struct {
	repoPath string
	commit   string
}

func (resolver Resolver) resolveRepositoryCommit(
	ctx context.Context,
	gitSource source.GitSource,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (string, string, error) {
	if resolver.session != nil {
		resolution, err := resolver.session.resolve(ctx, repositoryKey(gitSource), func() (repositoryResolution, error) {
			return resolver.resolveRepositorySnapshot(ctx, gitSource, sourceSpec, sourceID, options)
		})
		if err != nil {
			return "", "", err
		}
		return resolution.repoPath, resolution.commit, nil
	}

	resolution, err := resolver.resolveRepositorySnapshot(ctx, gitSource, sourceSpec, sourceID, options)
	if err != nil {
		return "", "", err
	}
	return resolution.repoPath, resolution.commit, nil
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
	options.Emit(acquisition.EventCacheWait, sourceSpec, sourceID, "", nil)
	if err := state.repoLocker.Do(ctx, key, func() error {
		repoPath, err := resolver.ensureRepository(ctx, gitSource, sourceSpec, sourceID, options)
		if err != nil {
			return err
		}

		commit, err := resolver.resolveCommit(ctx, repoPath, gitSource.Ref(), sourceSpec, sourceID, options)
		if err != nil {
			return err
		}

		resolution = repositoryResolution{
			repoPath: repoPath,
			commit:   commit,
		}
		return nil
	}); err != nil {
		return repositoryResolution{}, err
	}

	return resolution, nil
}

func (resolver Resolver) ensureRepository(
	ctx context.Context,
	gitSource source.GitSource,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (string, error) {
	locator := gitSource.Locator().String()
	if localPath, ok := gitSource.Locator().LocalPath(); ok {
		info, err := os.Stat(localPath)
		if err == nil && !info.IsDir() {
			return "", fmt.Errorf("git locator local input must be a repository directory; bundle files are unsupported")
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect local git locator: %w", err)
		}
	}
	repoPath := resolver.repositoryPath(locator)
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
		options.Emit(acquisition.EventCacheHit, sourceSpec, sourceID, "", nil)
		options.Emit(acquisition.EventFetch, sourceSpec, sourceID, "", nil)
		if err := resolver.runGit(ctx, repoPath, refreshArgs()...); err != nil {
			return "", fmt.Errorf("fetch git source %q: %w", locator, err)
		}

		return repoPath, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat git source cache %q: %w", repoPath, err)
	}

	if err := os.RemoveAll(repoPath); err != nil {
		return "", fmt.Errorf("remove stale git source cache %q: %w", repoPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(repoPath), 0o700); err != nil {
		return "", fmt.Errorf("create git source cache directory %q: %w", filepath.Dir(repoPath), err)
	}

	options.Emit(acquisition.EventFetch, sourceSpec, sourceID, "", nil)
	if err := resolver.runGit(ctx, "", cloneArgs(gitSource, repoPath)...); err != nil {
		return "", fmt.Errorf("clone git source %q: %w", locator, err)
	}

	return repoPath, nil
}

func (resolver Resolver) repositoryPath(url string) string {
	return filepath.Join(resolver.cacheRoot(), "repos", cacheKey(url))
}

func (resolver Resolver) artifactRoot(url string, commit string, gitPath string) string {
	return filepath.Join(resolver.artifactEntryRoot(url, commit, gitPath), "content")
}

func (resolver Resolver) artifactEntryRoot(url string, commit string, gitPath string) string {
	return filepath.Join(resolver.cacheRoot(), "artifacts", cacheKey(url), commit, cacheKey(gitPath))
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
