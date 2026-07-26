package journal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/supply/artifact"
)

type rootedTreeBackupSink struct {
	ctx            context.Context
	root           string
	hasher         *artifact.DirectoryHashBuilder
	directoryModes []rootedTreeBackupDirectoryMode
	initialized    bool
	finalized      bool
}

type rootedTreeBackupDirectoryMode struct {
	path string
	mode fs.FileMode
}

func newRootedTreeBackupSink(ctx context.Context, root string) *rootedTreeBackupSink {
	return &rootedTreeBackupSink{
		ctx:    ctx,
		root:   root,
		hasher: artifact.NewDirectoryHashBuilder(),
	}
}

func (sink *rootedTreeBackupSink) VisitRoot(mode fs.FileMode) error {
	if sink == nil || sink.initialized || sink.root == "" {
		return fmt.Errorf("rooted tree backup sink is not ready for a root")
	}
	if err := os.MkdirAll(filepath.Dir(sink.root), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(sink.root, 0o700); err != nil {
		return err
	}
	sink.directoryModes = append(sink.directoryModes, rootedTreeBackupDirectoryMode{
		path: sink.root,
		mode: mode.Perm(),
	})
	sink.initialized = true
	return nil
}

func (sink *rootedTreeBackupSink) VisitDirectory(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
	if !sink.initialized || sink.finalized {
		return fmt.Errorf("rooted tree backup root is not initialized")
	}
	if err := sink.hasher.AddDirectory(path.Path()); err != nil {
		return err
	}
	backupPath := filepath.Join(sink.root, filepath.FromSlash(path.Path()))
	if err := os.Mkdir(backupPath, 0o700); err != nil {
		return err
	}
	sink.directoryModes = append(sink.directoryModes, rootedTreeBackupDirectoryMode{
		path: backupPath,
		mode: mode.Perm(),
	})
	return nil
}

func (sink *rootedTreeBackupSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	if !sink.initialized || sink.finalized {
		return fmt.Errorf("rooted tree backup root is not initialized")
	}
	backupPath := filepath.Join(sink.root, filepath.FromSlash(path.Path()))
	file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	hashErr := sink.hasher.AddFile(
		sink.ctx,
		path.Path(),
		mode.Perm()&0o111 != 0,
		size,
		io.TeeReader(content, file),
	)
	if hashErr == nil {
		hashErr = file.Chmod(mode.Perm())
	}
	closeErr := file.Close()
	return errors.Join(hashErr, closeErr)
}

func (sink *rootedTreeBackupSink) hash() (artifact.ContentHash, error) {
	if sink == nil || !sink.initialized || sink.finalized {
		return "", fmt.Errorf("rooted tree backup is not initialized")
	}
	for index := len(sink.directoryModes) - 1; index >= 0; index-- {
		directory := sink.directoryModes[index]
		if err := os.Chmod(directory.path, directory.mode.Perm()); err != nil {
			return "", err
		}
	}
	sink.finalized = true
	return sink.hasher.Sum()
}
