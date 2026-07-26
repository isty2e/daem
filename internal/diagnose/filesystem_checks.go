package diagnose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/findings"
)

const probePrefix = ".daem-doctor-"

func directoryCheck(name string, path string) findings.Check {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return errorCheck(name, fmt.Sprintf("%s exists and is not a directory", path))
		}
		if _, err := os.ReadDir(path); err != nil {
			return errorCheck(name, fmt.Sprintf("%s is not readable: %v", path, err))
		}
		if err := probeWritableDirectory(path); err != nil {
			return errorCheck(name, fmt.Sprintf("%s is not writable: %v", path, err))
		}

		return okCheck(name, fmt.Sprintf("%s is readable and writable", path))
	}
	if !os.IsNotExist(err) {
		return errorCheck(name, fmt.Sprintf("stat %s: %v", path, err))
	}

	parent, err := nearestExistingDirectory(path)
	if err != nil {
		return errorCheck(name, fmt.Sprintf("%s cannot be created: %v", path, err))
	}
	if err := probeWritableDirectory(parent); err != nil {
		return errorCheck(name, fmt.Sprintf("%s cannot be created from %s: %v", path, parent, err))
	}

	return okCheck(name, fmt.Sprintf("%s can be created", path))
}

func nearestExistingDirectory(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("%s exists and is not a directory", current)
			}

			return current, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %s: %w", current, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing parent directory")
		}
		current = parent
	}
}

func probeWritableDirectory(directory string) error {
	tempFile, err := os.CreateTemp(directory, probePrefix)
	if err != nil {
		return err
	}

	tempPath := tempFile.Name()
	closeErr := tempFile.Close()
	removeErr := os.Remove(tempPath)
	return errors.Join(closeErr, removeErr)
}
