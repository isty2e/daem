package skill

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/supply/artifact"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func importSkillSourcePath(sourceDirectory adopt.SourceDirectory, installName string, contentHash artifact.ContentHash) (string, error) {
	return sourceDirectory.Resolve(filepath.Join(
		importSkillManifestSourceDirectoryName,
		installName,
		importSkillHashDirectoryName(contentHash),
	))
}

func resolvedImportSkillReadPath(livePath string) (string, error) {
	resolved, err := filepath.EvalSymlinks(livePath)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve imported skill path %q: %w", livePath, err)
	}
	return filepath.Clean(absolute), nil
}

func importSkillHashDirectoryName(contentHash artifact.ContentHash) string {
	return strings.ReplaceAll(string(contentHash), ":", "-")
}

func qualifiedImportSkillResourceName(target targetpkg.Target, scope targetpkg.Scope, installName string) string {
	targetName := strings.ReplaceAll(string(target), "-", "_")
	return targetName + "_" + string(scope) + "_" + installName
}

func uniqueImportSkillResourceName(name string, used map[string]int) string {
	if count := used[name]; count > 0 {
		used[name] = count + 1
		return fmt.Sprintf("%s_%d", name, count+1)
	}
	used[name] = 1
	return name
}

func cleanImportSkillName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "~") {
		return "", fmt.Errorf("skill name must be a safe single path segment")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("skill name must be a safe single path segment")
	}
	if filepath.Clean(name) != name {
		return "", fmt.Errorf("skill name must be a safe single path segment")
	}
	return name, nil
}

func suppliedSkillSkipReason(path string, name string) string {
	if name == ".system" || name == "system" {
		return importSkillSkipSuppliedEntry
	}
	parts := pathComponents(path)
	if slices.Contains(parts, ".system") {
		return importSkillSkipSuppliedEntry
	}
	for index := 0; index+2 < len(parts); index++ {
		if parts[index] == ".codex" && parts[index+1] == "plugins" && parts[index+2] == "cache" {
			return importSkillSkipSuppliedPluginCache
		}
	}
	return ""
}

func pathComponents(path string) []string {
	cleaned := filepath.Clean(path)
	volume := filepath.VolumeName(cleaned)
	cleaned = strings.TrimPrefix(cleaned, volume)
	cleaned = strings.Trim(cleaned, string(os.PathSeparator))
	if cleaned == "" {
		return nil
	}
	return strings.Split(cleaned, string(os.PathSeparator))
}

func firstNestedSymlink(root string) (string, bool, error) {
	var symlinkPath string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			symlinkPath = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}
	return symlinkPath, symlinkPath != "", nil
}
