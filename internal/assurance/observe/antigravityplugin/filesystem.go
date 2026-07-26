package antigravityplugin

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

const maximumInventoryBytes = 4 << 20

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
	content, exists, err := readStableRegularFile(manifest)
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

func readStableRegularFile(path string) ([]byte, bool, error) {
	before, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("file must not be a symlink")
	}
	if !before.Mode().IsRegular() {
		return nil, false, fmt.Errorf("path must be a regular file")
	}
	if before.Size() > maximumInventoryBytes {
		return nil, false, fmt.Errorf("file exceeds %d bytes", maximumInventoryBytes)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !os.SameFile(before, opened) ||
		before.Size() != opened.Size() ||
		!before.ModTime().Equal(opened.ModTime()) {
		return nil, false, fmt.Errorf("file changed while opening")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumInventoryBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(content) > maximumInventoryBytes {
		return nil, false, fmt.Errorf("file exceeds %d bytes", maximumInventoryBytes)
	}
	afterOpen, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	afterPath, err := os.Lstat(path)
	if err != nil {
		return nil, false, fmt.Errorf("reinspect file: %w", err)
	}
	if afterPath.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(opened, afterOpen) ||
		!os.SameFile(opened, afterPath) ||
		opened.Size() != afterOpen.Size() ||
		!opened.ModTime().Equal(afterOpen.ModTime()) {
		return nil, false, fmt.Errorf("file changed while reading")
	}
	return content, true, nil
}
