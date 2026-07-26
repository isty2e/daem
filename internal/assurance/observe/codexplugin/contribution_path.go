package codexplugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
)

var errPathBlocked = errors.New("path blocked")

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

func readBoundedFile(root string, path string) ([]byte, error) {
	switch regularBoundedFileReason(root, path) {
	case observecontribution.SourceContributionReasonNone:
		return os.ReadFile(path)
	case observecontribution.SourceContributionReasonArtifactPathBlocked, observecontribution.SourceContributionReasonUnsupportedShape:
		return nil, errPathBlocked
	default:
		return nil, os.ErrNotExist
	}
}

func isRegularBoundedFile(root string, path string) bool {
	return regularBoundedFileReason(root, path) == observecontribution.SourceContributionReasonNone
}

func regularBoundedFileReason(root string, path string) observecontribution.SourceContributionReason {
	if !pathWithin(root, path) {
		return observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	if pathHasSymlinkComponent(root, path) {
		return observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	info, err := os.Lstat(path)
	if err != nil {
		return observecontribution.SourceContributionReasonArtifactUnavailable
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	if !info.Mode().IsRegular() {
		return observecontribution.SourceContributionReasonUnsupportedShape
	}
	return observecontribution.SourceContributionReasonNone
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
