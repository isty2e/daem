package diagnose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/supply/artifact/access"
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
		if err := probeDirectoryOperations(path); err != nil {
			return errorCheck(name, fmt.Sprintf("%s cannot support daem storage operations: %v", path, err))
		}

		return okCheck(name, fmt.Sprintf("%s passed scratch storage and artifact-access checks", path))
	}
	if !os.IsNotExist(err) {
		return errorCheck(name, fmt.Sprintf("stat %s: %v", path, err))
	}

	parent, err := nearestExistingDirectory(path)
	if err != nil {
		return errorCheck(name, fmt.Sprintf("%s cannot be created: %v", path, err))
	}
	if err := probeDirectoryOperations(parent); err != nil {
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

func probeDirectoryOperations(directory string) (returnErr error) {
	scratch, err := os.MkdirTemp(directory, probePrefix)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, os.RemoveAll(scratch)) }()
	physical, err := filepath.EvalSymlinks(scratch)
	if err != nil {
		return err
	}

	ctx := context.Background()
	treePath := filepath.Join(physical, "created", "tree")
	root, destination, err := rootedpath.CaptureDestination(treePath)
	if err != nil {
		return err
	}
	defer root.Close()

	authority, err := root.Authority()
	if err != nil {
		return err
	}
	if _, err := authority.Provenance(); err != nil {
		return fmt.Errorf("capture recovery provenance: %w", err)
	}

	capability, err := root.Acquire(destination)
	if err != nil {
		return err
	}
	defer capability.Close()

	entry, err := mutationfs.NewTreeRelativePath("probe")
	if err != nil {
		return err
	}

	prepared, err := commit.PrepareRootedTree(ctx, capability, func(writer mutationfs.RootedTreeWriter) error {
		return writer.WriteFile(entry, 0o600, strings.NewReader("probe"))
	})
	if err != nil {
		return fmt.Errorf("prepare scratch tree: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, prepared.Abort(ctx)) }()
	if err := prepared.Commit(ctx); err != nil {
		return fmt.Errorf("publish scratch tree: %w", err)
	}

	view, err := access.OpenNoFollowView(treePath)
	if err != nil {
		return fmt.Errorf("open scratch artifact: %w", err)
	}
	if _, err := view.Hash(ctx); err != nil {
		return fmt.Errorf("read scratch artifact: %w", err)
	}

	filePath := filepath.Join(treePath, "probe")
	expected, err := commit.CaptureEntryIdentity(ctx, filePath)
	if err != nil {
		return err
	}
	replacement, err := commit.NewFileReplacement(filePath, []byte("updated probe"), 0o600, expected)
	if err != nil {
		return err
	}
	if err := commit.CommitFile(ctx, replacement); err != nil {
		return fmt.Errorf("replace scratch file: %w", err)
	}

	expected, err = commit.CaptureEntryIdentity(ctx, treePath)
	if err != nil {
		return err
	}
	removal, err := commit.NewLogicalRemoval(treePath, expected)
	if err != nil {
		return err
	}
	if err := commit.CommitLogicalRemoval(ctx, removal); err != nil {
		return fmt.Errorf("remove scratch tree: %w", err)
	}
	return nil
}
