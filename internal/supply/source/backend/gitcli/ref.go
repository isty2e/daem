package gitcli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func (resolver Resolver) resolveCommit(
	ctx context.Context,
	repository cachedRepository,
	selector source.GitRefSelector,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (commit string, returnErr error) {
	handle, err := resolver.openVerifiedRepository(ctx, repository)
	if err != nil {
		return "", err
	}
	defer func() {
		returnErr = errors.Join(returnErr, handle.Close())
	}()

	candidates := selector.ResolutionCandidates()
	if selector.IsCommit() {
		if commit, err := handle.verifyCommit(ctx, candidates[0]); err == nil {
			return validateResolvedCommit(sourceSpec, sourceID, commit)
		}

		options.Emit(acquisition.EventFetch, sourceSpec, sourceID, "", nil)
		if err := handle.runGit(ctx, fetchCommitArgs(selector.String())...); err != nil {
			return "", fmt.Errorf("fetch git ref %s: %w", selector.Canonical(), err)
		}
		if commit, err := handle.verifyCommit(ctx, candidates[0]); err == nil {
			return validateResolvedCommit(sourceSpec, sourceID, commit)
		}
		return "", fmt.Errorf("resolve git ref %s: fetched object is not a commit", selector.Canonical())
	}

	resolved := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		commit, err := handle.verifyCommit(ctx, candidate)
		if err == nil {
			commit, err = validateResolvedCommit(sourceSpec, sourceID, commit)
			if err != nil {
				return "", err
			}
			resolved = append(resolved, commit)
		}
	}

	switch len(resolved) {
	case 1:
		return resolved[0], nil
	case 2:
		return "", fmt.Errorf("resolve git ref %s: unqualified name matches both a branch and a tag", selector.Canonical())
	default:
		return "", fmt.Errorf("resolve git ref %s: no matching branch or tag", selector.Canonical())
	}
}

func (handle *repositoryHandle) verifyCommit(ctx context.Context, objectName string) (string, error) {
	commit, err := handle.gitOutput(ctx, verifyObjectArgs(objectName)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit), nil
}

func validateResolvedCommit(
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	commit string,
) (string, error) {
	if err := source.ValidateResolutionCorrelation(sourceSpec, sourceID, artifact.ResolvedRef(commit)); err != nil {
		return "", fmt.Errorf("validate resolved git commit: %w", err)
	}
	return commit, nil
}
