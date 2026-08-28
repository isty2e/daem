package antigravityplugin

import (
	"context"
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
	return observePluginBundleContext(context.Background(), paths, plugin, requireManifest)
}

func observePluginBundleContext(
	ctx context.Context,
	paths HostPaths,
	plugin string,
	requireManifest bool,
) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("Antigravity CLI plugin observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
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
	if err := ctx.Err(); err != nil {
		return false, err
	}
	manifest, err := paths.PluginManifestPath(plugin)
	if err != nil {
		return false, err
	}
	content, exists, err := filesnapshot.ReadRegularFileContext(
		ctx,
		manifest,
		MaximumInventoryBytes,
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
	if err := validatePluginManifestContext(ctx, content, plugin); err != nil {
		return false, fmt.Errorf("decode Antigravity CLI plugin manifest %q: %w", manifest, err)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	after, err := os.Lstat(directory)
	if err != nil ||
		after.Mode()&os.ModeSymlink != 0 ||
		!after.IsDir() ||
		!os.SameFile(before, after) {
		return false, fmt.Errorf("Antigravity CLI plugin directory %q changed during observation", directory)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}
