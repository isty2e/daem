package gitcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourcearchive "github.com/isty2e/daem/internal/supply/source/archive"
	sourcecache "github.com/isty2e/daem/internal/supply/source/cache"
)

func (resolver Resolver) ensureArtifact(
	ctx context.Context,
	repository cachedRepository,
	commit string,
	gitPath string,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (string, artifact.ContentHash, artifact.ArtifactKind, error) {
	state, err := resolver.requireState()
	if err != nil {
		return "", "", "", err
	}

	locator := repository.locator.String()
	key, err := cacheKeyForGitArtifact(locator, commit, gitPath)
	if err != nil {
		return "", "", "", err
	}

	entryRoot := resolver.artifactEntryRoot(locator, commit, gitPath)
	relativeContentPath := path.Join("content", gitPath)
	if gitPath == "." {
		relativeContentPath = "content"
	}
	spec, err := sourcecache.NewEntrySpec(key, relativeContentPath, "", "")
	if err != nil {
		return "", "", "", err
	}
	contentPath := filepath.Join(resolver.artifactRoot(locator, commit, gitPath), filepath.FromSlash(gitPath))
	if gitPath == "." {
		contentPath = resolver.artifactRoot(locator, commit, gitPath)
	}

	options.Emit(acquisition.EventCacheWait, sourceSpec, sourceID, artifact.ResolvedRef(commit), nil)
	cacheRoot, err := resolver.captureCacheRoot(ctx)
	if err != nil {
		return "", "", "", err
	}
	var contentHash artifact.ContentHash
	var contentKind artifact.ArtifactKind
	lockErr := state.artifactLocker.DoRooted(ctx, cacheRoot, key, func() error {
		verifiedHash, verifiedKind, published, err := sourcecache.PublishDirectoryOnceRooted(
			ctx,
			cacheRoot,
			resolver.artifactEntryRelativeRoot(locator, commit, gitPath),
			spec,
			func(tempEntryRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
				return resolver.buildArtifactEntry(
					ctx,
					repository,
					commit,
					gitPath,
					tempEntryRoot,
					sourceSpec,
					sourceID,
					options,
				)
			},
		)
		if err != nil {
			return fmt.Errorf("publish verified git artifact cache entry %q: %w", entryRoot, err)
		}
		contentHash = verifiedHash
		contentKind = verifiedKind

		if published {
			options.Emit(acquisition.EventPublished, sourceSpec, sourceID, artifact.ResolvedRef(commit), nil)
		} else {
			options.Emit(acquisition.EventCacheHit, sourceSpec, sourceID, artifact.ResolvedRef(commit), nil)
		}
		return nil
	})
	closeErr := cacheRoot.Close()
	if lockErr != nil || closeErr != nil {
		return "", "", "", errors.Join(lockErr, closeErr)
	}

	return contentPath, contentHash, contentKind, nil
}

func (resolver Resolver) buildArtifactEntry(
	ctx context.Context,
	repository cachedRepository,
	commit string,
	gitPath string,
	entryRoot string,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (artifact.ContentHash, artifact.ArtifactKind, error) {
	contentRoot := filepath.Join(entryRoot, "content")
	if err := os.MkdirAll(contentRoot, 0o700); err != nil {
		return "", "", fmt.Errorf("create git artifact content directory %q: %w", contentRoot, err)
	}

	options.Emit(acquisition.EventExport, sourceSpec, sourceID, artifact.ResolvedRef(commit), nil)
	if err := resolver.extractGitArchive(ctx, repository, commit, gitPath, contentRoot); err != nil {
		return "", "", fmt.Errorf("export git source path %q at %s: %w", gitPath, commit, err)
	}

	if resolver.state != nil && resolver.state.testAfterArchiveExtract != nil {
		resolver.state.testAfterArchiveExtract()
	}

	contentPath := filepath.Join(contentRoot, filepath.FromSlash(gitPath))
	if gitPath == "." {
		contentPath = contentRoot
	}
	if _, err := os.Lstat(contentPath); err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("git source path %q does not exist at %s", gitPath, commit)
		}

		return "", "", fmt.Errorf("stat exported git source path %q: %w", gitPath, err)
	}

	options.Emit(acquisition.EventHash, sourceSpec, sourceID, artifact.ResolvedRef(commit), nil)
	contentHash, artifactKind, err := access.HashPath(ctx, contentPath)
	if err != nil {
		return "", "", err
	}

	return contentHash, artifactKind, nil
}

func (resolver Resolver) extractGitArchive(
	ctx context.Context,
	repository cachedRepository,
	commit string,
	gitPath string,
	outputRoot string,
) error {
	handle, err := resolver.openVerifiedRepository(ctx, repository)
	if err != nil {
		return err
	}
	command, finish, err := handle.prepareCommand(ctx, archiveArgs(commit, gitPath))
	if err != nil {
		return errors.Join(err, handle.Close())
	}
	runErr := extractGitArchiveCommand(ctx, command, outputRoot)
	return errors.Join(runErr, finish(), handle.Close())
}

func extractGitArchiveCommand(ctx context.Context, command *exec.Cmd, outputRoot string) error {
	process, err := startGitProcess(command)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}

	extractErr, result := completeGitProcess(ctx, process, func(reader io.Reader) error {
		return sourcearchive.ExtractTar(ctx, reader, outputRoot)
	})
	if lifecycleErr := gitObservedLifecycleError(extractErr, result); lifecycleErr != nil {
		return joinGitProcessGroupTerminateErr(lifecycleErr, "git archive", result)
	}
	if extractErr != nil {
		if result.termination.UnsignalableOccupancy() || result.terminationErr != nil {
			return joinGitProcessGroupTerminateErr(extractErr, "git archive", result)
		}
		return extractErr
	}

	waitErr := result.stderrReadErr
	if result.commandErr != nil {
		waitErr = errors.Join(waitErr, result.commandErr)
	}
	waitErr = joinGitProcessGroupResidual(waitErr, "git archive", result)
	if waitErr != nil {
		return waitErr
	}

	return nil
}
