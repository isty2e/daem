package codexplugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	"github.com/isty2e/daem/internal/filesnapshot"
)

func resolveManifestPath(pluginRoot string, manifestPath string) (string, string, observecontribution.SourceContributionReason) {
	if observecontribution.ContainsUnsafeDiagnosticRune(manifestPath) {
		return "", "", observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	if !strings.HasPrefix(manifestPath, "./") {
		return "", "", observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	relative := strings.TrimPrefix(manifestPath, "./")
	if relative == "" {
		return "", "", observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	cleanSlash := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if cleanSlash == "." || strings.HasPrefix(cleanSlash, "../") || cleanSlash == ".." || filepath.IsAbs(cleanSlash) {
		return "", "", observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	resolved := filepath.Join(pluginRoot, filepath.FromSlash(cleanSlash))
	if !pathWithin(pluginRoot, resolved) {
		return "", "", observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	return resolved, cleanSlash, observecontribution.SourceContributionReasonNone
}

func snapshotContainedFile(
	ctx context.Context,
	root string,
	path string,
) (content []byte, exists bool, reason observecontribution.SourceContributionReason, err error) {
	if ctx == nil {
		return nil, false, observecontribution.SourceContributionReasonNone, errors.New("Codex plugin observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, observecontribution.SourceContributionReasonNone, err
	}
	if !pathWithin(root, path) || pathHasSymlinkComponent(root, path) {
		return nil, false, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	}
	content, exists, err = filesnapshot.ReadRegularFileContext(ctx, path, MaximumContributionFileBytes)
	if err != nil {
		return nil, false, classifySnapshotError(err), snapshotObservationError(err)
	}
	return content, exists, observecontribution.SourceContributionReasonNone, nil
}

func requiredContainedFile(
	ctx context.Context,
	root string,
	path string,
) (content []byte, reason observecontribution.SourceContributionReason, err error) {
	content, exists, reason, err := snapshotContainedFile(ctx, root, path)
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, err
	}
	if !exists {
		return nil, observecontribution.SourceContributionReasonArtifactUnavailable, nil
	}
	return content, observecontribution.SourceContributionReasonNone, nil
}

func classifySnapshotError(err error) observecontribution.SourceContributionReason {
	switch {
	case err == nil, observationCanceled(err):
		return observecontribution.SourceContributionReasonNone
	case errors.Is(err, filesnapshot.ErrChanged):
		return observecontribution.SourceContributionReasonArtifactUnstable
	case errors.Is(err, filesnapshot.ErrLimitExceeded):
		return observecontribution.SourceContributionReasonArtifactBudgetExceeded
	case errors.Is(err, filesnapshot.ErrSymlink):
		return observecontribution.SourceContributionReasonArtifactPathBlocked
	case errors.Is(err, filesnapshot.ErrNotRegular):
		return observecontribution.SourceContributionReasonUnsupportedShape
	default:
		return observecontribution.SourceContributionReasonArtifactUnavailable
	}
}

func snapshotObservationError(err error) error {
	if observationCanceled(err) {
		return err
	}
	return nil
}

func listContainedDirectoryNames(
	ctx context.Context,
	root string,
	path string,
	budget *observationBudget,
) (names []string, reason observecontribution.SourceContributionReason, err error) {
	if ctx == nil {
		return nil, observecontribution.SourceContributionReasonNone, errors.New("Codex plugin observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, observecontribution.SourceContributionReasonNone, err
	}
	if budget == nil || budget.exceeded {
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	if !pathWithin(root, path) || pathHasSymlinkComponent(root, path) {
		return nil, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	}
	remaining := budget.remainingEntries()
	if remaining == 0 {
		budget.exhaust()
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	names, err = readDirectoryNamesUpTo(ctx, path, remaining)
	if err != nil {
		if observationCanceled(err) {
			return nil, observecontribution.SourceContributionReasonNone, err
		}
		if directoryPathBlocked(err) {
			return nil, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
		}
		return nil, observecontribution.SourceContributionReasonNone, err
	}
	if len(names) > remaining || budget.consumeNames(names) {
		budget.exhaust()
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	return names, observecontribution.SourceContributionReasonNone, nil
}

func observationCanceled(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func directoryMissing(err error) bool {
	return err != nil && !observationCanceled(err) && errors.Is(err, os.ErrNotExist)
}

func pathWithin(root string, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return relative == "." || (!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != ".." && !filepath.IsAbs(relative))
}

func pathHasSymlinkComponent(root string, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." {
		return false
	}
	if strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." || filepath.IsAbs(relative) {
		return true
	}

	current := root
	for part := range strings.SplitSeq(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}
