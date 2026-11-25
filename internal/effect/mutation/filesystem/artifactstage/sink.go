package artifactstage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

// New bridges exact artifact traversal into caller-owned rooted mutation
// staging. The returned sink never publishes the staged tree.
func New(writer mutationfs.RootedTreeWriter) (Sink, error) {
	if writer == nil {
		return Sink{}, fmt.Errorf("rooted artifact stage writer is required")
	}
	return Sink{writer: writer}, nil
}

// Sink adapts artifact traversal callbacks to one unpublished rooted writer.
// Interface ownership remains with the caller; this package imports no Supply
// protocol.
type Sink struct {
	writer  mutationfs.RootedTreeWriter
	modeFor func(string, fs.FileMode) fs.FileMode
}

// ForInstallation keeps the modes of existing entries of the same kind. A
// newly installed regular file inherits only its source execution class.
func (stage Sink) ForInstallation(previous map[string]fs.FileMode) Sink {
	stage.modeFor = func(path string, source fs.FileMode) fs.FileMode {
		if mode, ok := previous[path]; ok && mode.IsDir() == source.IsDir() {
			return mode.Perm()
		}
		if source.IsDir() {
			return source.Perm()
		}
		if source.Perm()&0o111 != 0 {
			return 0o700
		}
		return 0o600
	}
	return stage
}

// BeginDirectory creates one relative directory or sets the stage root mode.
func (stage Sink) BeginDirectory(relativePath string, mode fs.FileMode) error {
	if stage.modeFor != nil {
		mode = stage.modeFor(relativePath, mode.Perm()|fs.ModeDir)
	}
	if relativePath == "." {
		return stage.writer.SetRootMode(mode.Perm())
	}
	path, err := rootedTreePath(relativePath)
	if err != nil {
		return err
	}
	return stage.writer.CreateDirectory(path, mode.Perm())
}

// EndDirectory completes a traversal directory with no additional effect.
func (Sink) EndDirectory(string, fs.FileMode) error {
	return nil
}

// OpenFile streams one relative regular file into rooted staging.
func (stage Sink) OpenFile(
	relativePath string,
	mode fs.FileMode,
	size int64,
) (io.WriteCloser, error) {
	if stage.modeFor != nil {
		mode = stage.modeFor(relativePath, mode.Perm())
	}
	if size < 0 {
		return nil, fmt.Errorf("rooted artifact stage file size must be non-negative")
	}
	path, err := rootedTreePath(relativePath)
	if err != nil {
		return nil, err
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		writeErr := stage.writer.WriteFile(path, mode.Perm(), reader)
		_ = reader.CloseWithError(writeErr)
		done <- writeErr
		close(done)
	}()
	return &stagedFile{writer: writer, done: done}, nil
}

func rootedTreePath(relativePath string) (mutationfs.TreeRelativePath, error) {
	if relativePath == "." {
		return mutationfs.TreeRelativePath{}, fmt.Errorf(
			"rooted artifact stage entry cannot name the root",
		)
	}
	return mutationfs.NewTreeRelativePath(strings.Split(relativePath, "/")...)
}

type stagedFile struct {
	writer *io.PipeWriter
	done   <-chan error
	once   sync.Once
	err    error
}

func (file *stagedFile) Write(content []byte) (int, error) {
	return file.writer.Write(content)
}

func (file *stagedFile) Close() error {
	file.once.Do(func() {
		file.err = errors.Join(file.writer.Close(), <-file.done)
	})
	return file.err
}
