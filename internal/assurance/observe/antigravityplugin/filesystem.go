package antigravityplugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/isty2e/daem/internal/filesnapshot"
)

func observePluginBundle(
	paths HostPaths,
	plugin string,
	requireManifest bool,
) (bool, error) {
	directory, err := paths.PluginDirectoryPath(plugin)
	if err != nil {
		return false, err
	}
	before, err := os.Lstat(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect Antigravity CLI plugin directory %q: %w", directory, err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return false, fmt.Errorf(
			"Antigravity CLI plugin path %q is not a non-symlink directory",
			directory,
		)
	}
	manifest, err := paths.PluginManifestPath(plugin)
	if err != nil {
		return false, err
	}
	content, exists, err := filesnapshot.ReadRegularFile(
		manifest,
		maximumInventoryBytes,
	)
	if err != nil {
		return false, fmt.Errorf("read Antigravity CLI plugin manifest %q: %w", manifest, err)
	}
	if !exists {
		if !requireManifest {
			return true, nil
		}
		return false, fmt.Errorf("Antigravity CLI plugin directory %q has no plugin.json", directory)
	}
	if err := validatePluginManifest(content, plugin); err != nil {
		return false, fmt.Errorf("decode Antigravity CLI plugin manifest %q: %w", manifest, err)
	}
	after, err := os.Lstat(directory)
	if err != nil ||
		after.Mode()&os.ModeSymlink != 0 ||
		!after.IsDir() ||
		!os.SameFile(before, after) {
		return false, fmt.Errorf("Antigravity CLI plugin directory %q changed during observation", directory)
	}
	return true, nil
}
