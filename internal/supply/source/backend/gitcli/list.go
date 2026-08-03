package gitcli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
) (listing source.RootListing, returnErr error) {
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
	budget := options.RootListingBudget()

	snapshot, err := resolver.resolveRepositoryCommit(ctx, gitSource, sourceSpec, sourceID, options)
	if err != nil {
		return source.RootListing{}, err
	}
	handle, err := resolver.openVerifiedRepository(ctx, snapshot.repository)
	if err != nil {
		return source.RootListing{}, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, handle.Close())
	}()

	objectName := gitObjectName(snapshot.commit, gitPath)
	objectKindOutput, err := handle.gitOutput(ctx, inspectObjectArgs(objectName)...)
	if err != nil {
		return source.RootListing{}, fmt.Errorf("git source path %q does not exist at %s", gitPath, snapshot.commit)
	}

	switch objectKind := strings.TrimSpace(objectKindOutput); objectKind {
	case "tree":
		childNames, err := handle.listTreeDirectories(ctx, objectName, budget)
		if err != nil {
			return source.RootListing{}, fmt.Errorf("list git source path %q at %s: %w", gitPath, snapshot.commit, err)
		}
		if err := ctx.Err(); err != nil {
			return source.RootListing{}, err
		}

		return source.NewRootListing(
			sourceSpec,
			artifact.ResolvedRef(snapshot.commit),
			artifact.ArtifactKindDirectory,
			childNames,
		)
	case "blob":
		return source.NewRootListing(sourceSpec, artifact.ResolvedRef(snapshot.commit), artifact.ArtifactKindFile, nil)
	default:
		return source.RootListing{}, fmt.Errorf(
			"git source path %q at %s has unsupported object kind %q",
			gitPath,
			snapshot.commit,
			objectKind,
		)
	}
}

func (handle *repositoryHandle) listTreeDirectories(
	ctx context.Context,
	objectName string,
	budget *source.RootListingBudget,
) ([]string, error) {
	var childNames []string
	err := handle.consumeGitOutput(ctx, func(output io.Reader) error {
		var err error
		childNames, err = readGitTreeDirectories(output, budget)
		return err
	}, listTreeArgs(objectName)...)
	if err != nil {
		return nil, err
	}
	return childNames, nil
}

func readGitTreeDirectories(
	output io.Reader,
	budget *source.RootListingBudget,
) ([]string, error) {
	childNames := make([]string, 0)
	maximumEntryNameBytes := budget.MaximumEntryNameBytes()
	readerSize := int(maximumEntryNameBytes) + 256
	reader := bufio.NewReaderSize(output, readerSize)
	for {
		record, readErr := reader.ReadSlice(0)
		if errors.Is(readErr, io.EOF) && len(record) == 0 {
			return childNames, nil
		}
		if errors.Is(readErr, bufio.ErrBufferFull) {
			return nil, budget.AdmitEntryName(int(maximumEntryNameBytes) + 1)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("git tree listing ended without a NUL record terminator")
			}
			return nil, readErr
		}

		name, directory, err := parseGitTreeRecord(record[:len(record)-1])
		if err != nil {
			return nil, err
		}
		if err := budget.AdmitEntryName(len(name)); err != nil {
			return nil, err
		}
		if directory {
			childNames = append(childNames, name)
		}
	}
}

func parseGitTreeRecord(record []byte) (name string, directory bool, err error) {
	metadata, nameBytes, ok := strings.Cut(string(record), "\t")
	if !ok || len(nameBytes) == 0 {
		return "", false, fmt.Errorf("git tree listing contains a malformed record")
	}
	fields := strings.Fields(metadata)
	if len(fields) != 3 || fields[0] == "" || fields[2] == "" {
		return "", false, fmt.Errorf("git tree listing contains malformed metadata")
	}
	switch fields[1] {
	case "tree":
		return nameBytes, true, nil
	case "blob", "commit":
		return nameBytes, false, nil
	default:
		return "", false, fmt.Errorf("git tree listing contains unsupported object kind %q", fields[1])
	}
}

func gitObjectName(commit string, gitPath string) string {
	if gitPath == "." {
		return commit + "^{tree}"
	}

	return commit + ":" + gitPath
}
