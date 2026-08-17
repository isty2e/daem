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

func resolveManifestPath(manifestPath string) (string, observecontribution.SourceContributionReason) {
	if observecontribution.ContainsUnsafeDiagnosticRune(manifestPath) {
		return "", observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	if !strings.HasPrefix(manifestPath, "./") {
		return "", observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	relative := strings.TrimPrefix(manifestPath, "./")
	if relative == "" {
		return "", observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	cleanSlash := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if cleanSlash == "." || strings.HasPrefix(cleanSlash, "../") || cleanSlash == ".." || filepath.IsAbs(cleanSlash) {
		return "", observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	return cleanSlash, observecontribution.SourceContributionReasonNone
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

func observationCanceled(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func directoryMissing(err error) bool {
	return err != nil && !observationCanceled(err) && errors.Is(err, os.ErrNotExist)
}
