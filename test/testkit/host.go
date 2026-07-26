package testkit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ReplaceDirectoryAtomic(sourcePath string, destinationPath string) error {
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
		return err
	}
	tempPath, err := os.MkdirTemp(filepath.Dir(destinationPath), "."+filepath.Base(destinationPath)+".*.tmp")
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(tempPath)
		}
	}()

	if err := CopyDirectory(sourcePath, tempPath); err != nil {
		return err
	}
	if err := RemoveHostPath(destinationPath); err != nil {
		return err
	}
	if err := os.Rename(tempPath, destinationPath); err != nil {
		return err
	}

	committed = true
	return nil
}

func RemoveHostPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(path)
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("unsupported file mode %s", info.Mode())
	}

	return os.Remove(path)
}

func CopyDirectory(sourcePath string, destinationPath string) error {
	sourceRoot := filepath.Clean(sourcePath)
	destinationRoot := filepath.Clean(destinationPath)
	if directoryContains(sourceRoot, destinationRoot) {
		return fmt.Errorf("copy destination %q must not be inside source directory %q", destinationRoot, sourceRoot)
	}
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path %q is a symlink; symlinks are not supported", path)
		}

		relativePath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return fmt.Errorf("compute relative path for %q: %w", path, err)
		}
		destination := filepath.Join(destinationRoot, relativePath)
		if entry.IsDir() {
			if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create directory %q: %w", destination, err)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("path %q has unsupported file mode %s", path, info.Mode())
		}
		if err := copyFilePreservingMode(path, destination, info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy file %q: %w", path, err)
		}

		return nil
	})
}

func directoryContains(parent string, child string) bool {
	relativePath, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relativePath == "." || (relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator)))
}

func copyFilePreservingMode(sourcePath string, destinationPath string, fileMode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
		return err
	}
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		destinationFile.Close()
		return err
	}
	if err := destinationFile.Close(); err != nil {
		return err
	}

	return nil
}
