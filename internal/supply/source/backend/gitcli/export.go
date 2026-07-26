package gitcli

import (
	"context"
	"errors"
	"fmt"
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
	url string,
	repoPath string,
	commit string,
	gitPath string,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (string, error) {
	state, err := resolver.requireState()
	if err != nil {
		return "", err
	}

	key, err := cacheKeyForGitArtifact(url, commit, gitPath)
	if err != nil {
		return "", err
	}

	entryRoot := resolver.artifactEntryRoot(url, commit, gitPath)
	relativeContentPath := path.Join("content", gitPath)
	if gitPath == "." {
		relativeContentPath = "content"
	}
	spec, err := sourcecache.NewEntrySpec(key, relativeContentPath, "", "")
	if err != nil {
		return "", err
	}
	contentPath := filepath.Join(resolver.artifactRoot(url, commit, gitPath), filepath.FromSlash(gitPath))
	if gitPath == "." {
		contentPath = resolver.artifactRoot(url, commit, gitPath)
	}

	options.Emit(acquisition.EventCacheWait, sourceSpec, sourceID, artifact.ResolvedRef(commit), nil)
	err = state.artifactLocker.Do(ctx, key, func() error {
		published, err := sourcecache.PublishDirectoryOnce(ctx, entryRoot, spec, func(tempEntryRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
			return resolver.buildArtifactEntry(ctx, repoPath, commit, gitPath, tempEntryRoot, sourceSpec, sourceID, options)
		})
		if err != nil {
			return fmt.Errorf("publish verified git artifact cache entry %q: %w", entryRoot, err)
		}

		if published {
			options.Emit(acquisition.EventPublished, sourceSpec, sourceID, artifact.ResolvedRef(commit), nil)
		} else {
			options.Emit(acquisition.EventCacheHit, sourceSpec, sourceID, artifact.ResolvedRef(commit), nil)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	return contentPath, nil
}

func (resolver Resolver) buildArtifactEntry(
	ctx context.Context,
	repoPath string,
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
	if err := resolver.extractGitArchive(ctx, repoPath, commit, gitPath, contentRoot); err != nil {
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

func (resolver Resolver) extractGitArchive(ctx context.Context, repoPath string, commit string, gitPath string, outputRoot string) error {
	command := exec.CommandContext(ctx, gitExecutable, archiveArgs(commit, gitPath)...)
	command.Dir = repoPath
	return extractGitArchiveCommand(ctx, command, outputRoot)
}

func extractGitArchiveCommand(ctx context.Context, command *exec.Cmd, outputRoot string) error {
	process, err := startGitProcess(command)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}

	extractErr := sourcearchive.ExtractTar(ctx, process.Stdout(), outputRoot)
	if extractErr != nil {
		_, _ = process.Terminate()
	}

	result := process.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		if result.terminationErr != nil {
			return errors.Join(ctxErr, result.terminationErr)
		}
		return ctxErr
	}
	if extractErr != nil {
		if result.terminationErr != nil {
			return errors.Join(extractErr, fmt.Errorf("terminate git archive process tree: %w", result.terminationErr))
		}
		return extractErr
	}

	waitErr := result.stderrReadErr
	if result.waitErr != nil {
		waitErr = errors.Join(
			waitErr,
			gitCommandErrorWithCapture(result.waitErr, result.stderr, result.stderrTruncated),
		)
	}
	if result.terminationErr != nil {
		waitErr = errors.Join(waitErr, fmt.Errorf("terminate git archive process tree: %w", result.terminationErr))
	}
	if result.termination.ProcessesFound() {
		waitErr = errors.Join(waitErr, errors.New("git archive exited while descendant processes remained; terminated residual process tree"))
	}
	if waitErr != nil {
		return waitErr
	}

	return nil
}
