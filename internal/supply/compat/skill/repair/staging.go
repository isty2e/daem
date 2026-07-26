package repair

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

type repairStaging struct {
	temporaryRoot  string
	artifactRoot   string
	directoryModes map[string]fs.FileMode
}

func newVerifiedRepairStaging(
	ctx context.Context,
	identity artifact.ExactIdentity,
	view access.View,
) (repairStaging, error) {
	if identity.Kind() != artifact.ArtifactKindDirectory || view.Kind() != artifact.ArtifactKindDirectory {
		return repairStaging{}, fmt.Errorf("skill repair staging requires a directory artifact")
	}
	temporaryRoot, err := os.MkdirTemp("", "daem-skill-repair-")
	if err != nil {
		return repairStaging{}, fmt.Errorf("create temporary skill repair directory: %w", err)
	}
	staging := repairStaging{
		temporaryRoot:  temporaryRoot,
		artifactRoot:   filepath.Join(temporaryRoot, "artifact"),
		directoryModes: make(map[string]fs.FileMode),
	}
	sink := repairTreeSink{root: staging.artifactRoot, directoryModes: staging.directoryModes}
	if err := view.CopyVerified(ctx, identity, sink); err != nil {
		return repairStaging{}, errors.Join(
			fmt.Errorf("copy verified skill source for repair: %w", err),
			staging.release(),
		)
	}
	return staging, nil
}

func (staging repairStaging) openView() (access.View, error) {
	return access.OpenView(staging.artifactRoot)
}

func (staging *repairStaging) materialization() (*access.Materialization, error) {
	if staging == nil || staging.temporaryRoot == "" {
		return nil, fmt.Errorf("skill repair staging is unavailable")
	}
	view, err := staging.openView()
	if err != nil {
		return nil, errors.Join(err, staging.release())
	}
	temporaryRoot := staging.temporaryRoot
	artifactRoot := staging.artifactRoot
	directoryPaths := staging.directoryPaths()
	materialization, err := access.NewMaterialization(view, func() error {
		return removeRepairStaging(temporaryRoot, artifactRoot, directoryPaths)
	})
	if err != nil {
		return nil, errors.Join(err, staging.release())
	}
	staging.temporaryRoot = ""
	staging.artifactRoot = ""
	staging.directoryModes = nil
	return materialization, nil
}

func (staging *repairStaging) finalizeDirectoryModes() error {
	if staging == nil || staging.temporaryRoot == "" {
		return fmt.Errorf("skill repair staging is unavailable")
	}
	paths := staging.directoryPaths()
	sort.Slice(paths, func(left int, right int) bool {
		leftDepth := strings.Count(paths[left], "/")
		rightDepth := strings.Count(paths[right], "/")
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return paths[left] > paths[right]
	})
	for _, relativePath := range paths {
		target := staging.artifactRoot
		if relativePath != "." {
			var err error
			target, err = artifactPath(staging.artifactRoot, relativePath)
			if err != nil {
				return err
			}
		}
		info, err := os.Lstat(target)
		if err != nil {
			return fmt.Errorf("inspect repair staging directory %q: %w", relativePath, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("repair staging path %q is not a directory", relativePath)
		}
		if err := os.Chmod(target, staging.directoryModes[relativePath].Perm()); err != nil {
			return fmt.Errorf("restore repair staging directory mode %q: %w", relativePath, err)
		}
	}
	return nil
}

func (staging *repairStaging) release() error {
	if staging == nil || staging.temporaryRoot == "" {
		return nil
	}
	err := removeRepairStaging(
		staging.temporaryRoot,
		staging.artifactRoot,
		staging.directoryPaths(),
	)
	staging.temporaryRoot = ""
	staging.artifactRoot = ""
	staging.directoryModes = nil
	return err
}

func (staging *repairStaging) directoryPaths() []string {
	paths := make([]string, 0, len(staging.directoryModes))
	for relativePath := range staging.directoryModes {
		paths = append(paths, relativePath)
	}
	return paths
}

func removeRepairStaging(temporaryRoot string, artifactRoot string, directoryPaths []string) error {
	sort.Slice(directoryPaths, func(left int, right int) bool {
		leftDepth := strings.Count(directoryPaths[left], "/")
		rightDepth := strings.Count(directoryPaths[right], "/")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return directoryPaths[left] < directoryPaths[right]
	})
	errorsToJoin := make([]error, 0, len(directoryPaths)+1)
	for _, relativePath := range directoryPaths {
		target := artifactRoot
		if relativePath != "." {
			var err error
			target, err = artifactPath(artifactRoot, relativePath)
			if err != nil {
				errorsToJoin = append(errorsToJoin, err)
				continue
			}
		}
		info, err := os.Lstat(target)
		if err != nil {
			if !os.IsNotExist(err) {
				errorsToJoin = append(errorsToJoin, fmt.Errorf("inspect repair cleanup directory %q: %w", relativePath, err))
			}
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			errorsToJoin = append(errorsToJoin, fmt.Errorf("repair cleanup path %q is not a directory", relativePath))
			continue
		}
		if err := os.Chmod(target, 0o700); err != nil {
			errorsToJoin = append(errorsToJoin, fmt.Errorf("make repair cleanup directory writable %q: %w", relativePath, err))
		}
	}
	errorsToJoin = append(errorsToJoin, os.RemoveAll(temporaryRoot))
	return errors.Join(errorsToJoin...)
}

type repairTreeSink struct {
	root           string
	directoryModes map[string]fs.FileMode
}

func (sink repairTreeSink) BeginDirectory(relativePath string, _ fs.FileMode) error {
	target, err := sink.path(relativePath)
	if err != nil {
		return err
	}
	if err := requireStagingParent(target, sink.root, relativePath); err != nil {
		return err
	}
	if err := os.Mkdir(target, 0o700); err != nil {
		return fmt.Errorf("create repair staging directory %q: %w", relativePath, err)
	}
	return nil
}

func (sink repairTreeSink) OpenFile(
	relativePath string,
	mode fs.FileMode,
	size int64,
) (io.WriteCloser, error) {
	if size < 0 {
		return nil, fmt.Errorf("repair staging file %q has negative size", relativePath)
	}
	target, err := sink.path(relativePath)
	if err != nil {
		return nil, err
	}
	if err := requireStagingParent(target, sink.root, relativePath); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create repair staging file %q: %w", relativePath, err)
	}
	return &repairStagingFile{file: file, mode: mode.Perm()}, nil
}

func (sink repairTreeSink) EndDirectory(relativePath string, mode fs.FileMode) error {
	target, err := sink.path(relativePath)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("inspect repair staging directory %q: %w", relativePath, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("repair staging path %q is not a directory", relativePath)
	}
	if sink.directoryModes == nil {
		return fmt.Errorf("repair staging directory mode registry is unavailable")
	}
	sink.directoryModes[relativePath] = mode.Perm()
	return nil
}

func (sink repairTreeSink) path(relativePath string) (string, error) {
	if relativePath == "." {
		return sink.root, nil
	}
	if err := validateRecipePath(relativePath); err != nil {
		return "", err
	}
	return filepath.Join(sink.root, filepath.FromSlash(relativePath)), nil
}

type repairStagingFile struct {
	file *os.File
	mode fs.FileMode
}

func (file *repairStagingFile) Write(content []byte) (int, error) {
	if file == nil || file.file == nil {
		return 0, fmt.Errorf("repair staging file is closed")
	}
	return file.file.Write(content)
}

func (file *repairStagingFile) Close() error {
	if file == nil || file.file == nil {
		return nil
	}
	chmodErr := file.file.Chmod(file.mode)
	closeErr := file.file.Close()
	file.file = nil
	return errors.Join(chmodErr, closeErr)
}

func requireStagingParent(target string, root string, relativePath string) error {
	if target == root {
		return nil
	}
	parent := filepath.Dir(target)
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect repair staging parent for %q: %w", relativePath, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("repair staging parent for %q is not a directory", relativePath)
	}
	return nil
}
