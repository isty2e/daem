package gitcli

import (
	"context"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func (resolver Resolver) resolveCommit(
	ctx context.Context,
	repoPath string,
	selector source.GitRefSelector,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	options acquisition.OperationOptions,
) (string, error) {
	candidates := selector.ResolutionCandidates()
	if selector.IsCommit() {
		if commit, err := resolver.verifyCommit(ctx, repoPath, candidates[0]); err == nil {
			return validateResolvedCommit(sourceSpec, sourceID, commit)
		}

		options.Emit(acquisition.EventFetch, sourceSpec, sourceID, "", nil)
		if err := resolver.runGit(ctx, repoPath, fetchCommitArgs(selector.String())...); err != nil {
			return "", fmt.Errorf("fetch git ref %s: %w", selector.Canonical(), err)
		}
		if commit, err := resolver.verifyCommit(ctx, repoPath, candidates[0]); err == nil {
			return validateResolvedCommit(sourceSpec, sourceID, commit)
		}
		return "", fmt.Errorf("resolve git ref %s: fetched object is not a commit", selector.Canonical())
	}

	resolved := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		commit, err := resolver.verifyCommit(ctx, repoPath, candidate)
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

func (resolver Resolver) verifyCommit(ctx context.Context, repoPath string, objectName string) (string, error) {
	commit, err := resolver.gitOutput(ctx, repoPath, verifyObjectArgs(objectName)...)
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
