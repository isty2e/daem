package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

// ListSourceRoot lists direct child directories of a Git source root without exporting or hashing the root tree.
func (resolver Resolver) ListSourceRoot(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (source.RootListing, error) {
	if ctx == nil {
		return source.RootListing{}, fmt.Errorf("git root listing context is required")
	}
	if err := ctx.Err(); err != nil {
		return source.RootListing{}, err
	}

	gitSource, ok := sourceSpec.Git()
	if !ok {
		return source.RootListing{}, fmt.Errorf("git resolver only supports git sources, got %q", sourceSpec.Kind())
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return source.RootListing{}, err
	}

	gitPath := gitSource.RepositoryPath().String()

	repoPath, commit, err := resolver.resolveRepositoryCommit(ctx, gitSource, sourceSpec, sourceID, options)
	if err != nil {
		return source.RootListing{}, err
	}

	objectName := gitObjectName(commit, gitPath)
	objectKindOutput, err := resolver.gitOutput(ctx, repoPath, inspectObjectArgs(objectName)...)
	if err != nil {
		return source.RootListing{}, fmt.Errorf("git source path %q does not exist at %s", gitPath, commit)
	}

	switch objectKind := strings.TrimSpace(objectKindOutput); objectKind {
	case "tree":
		childNames, err := resolver.listTreeDirectories(ctx, repoPath, objectName)
		if err != nil {
			return source.RootListing{}, fmt.Errorf("list git source path %q at %s: %w", gitPath, commit, err)
		}
		if err := ctx.Err(); err != nil {
			return source.RootListing{}, err
		}

		return source.NewRootListing(
			sourceSpec,
			artifact.ResolvedRef(commit),
			artifact.ArtifactKindDirectory,
			childNames,
		)
	case "blob":
		return source.NewRootListing(sourceSpec, artifact.ResolvedRef(commit), artifact.ArtifactKindFile, nil)
	default:
		return source.RootListing{}, fmt.Errorf("git source path %q at %s has unsupported object kind %q", gitPath, commit, objectKind)
	}
}

func (resolver Resolver) listTreeDirectories(ctx context.Context, repoPath string, objectName string) ([]string, error) {
	output, err := resolver.gitBytes(ctx, repoPath, listTreeArgs(objectName)...)
	if err != nil {
		return nil, err
	}

	parts := bytes.Split(output, []byte{0})
	childNames := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		childNames = append(childNames, string(part))
	}
	return childNames, nil
}

func gitObjectName(commit string, gitPath string) string {
	if gitPath == "." {
		return commit + "^{tree}"
	}

	return commit + ":" + gitPath
}
