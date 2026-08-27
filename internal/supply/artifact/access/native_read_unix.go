//go:build darwin || linux

package access

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"sort"

	"github.com/isty2e/daem/internal/supply/artifact"
	"golang.org/x/sys/unix"
)

func inspectNative(root string) (result artifact.ArtifactKind, resultErr error) {
	handle, err := openNativeRoot(root, "")
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.close()) }()
	switch handle.entry.kind {
	case nativeKindFile:
		return artifact.ArtifactKindFile, nil
	case nativeKindDirectory:
		return artifact.ArtifactKindDirectory, nil
	default:
		return "", fmt.Errorf("artifact access root has unsupported kind")
	}
}

func readDirectoryNative(
	ctx context.Context,
	root string,
	expectedKind artifact.ArtifactKind,
	relativePath string,
) ([]Entry, error) {
	entries := make([]Entry, 0)
	if err := visitDirectoryNative(ctx, root, expectedKind, relativePath, func(entry Entry) error {
		entries = append(entries, entry)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(left int, right int) bool {
		return entries[left].name < entries[right].name
	})
	return entries, nil
}

func visitDirectoryNative(
	ctx context.Context,
	root string,
	expectedKind artifact.ArtifactKind,
	relativePath string,
	visit func(Entry) error,
) error {
	return visitOpenedNativeDirectory(ctx, root, expectedKind, relativePath, func(entry nativeEntry) error {
		return visitNativeDirectoryNames(entry.fd, func(name string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			observed, stat, err := observeNativeEntry(entry.fd, name)
			if err != nil {
				return err
			}
			return visit(Entry{
				name: name,
				kind: publicEntryKind(observed.kind),
				mode: fs.FileMode(stat.Mode & 0o777),
			})
		})
	})
}

func visitDirectoryNamesNative(
	ctx context.Context,
	root string,
	expectedKind artifact.ArtifactKind,
	relativePath string,
	visit func(string) error,
) error {
	return visitOpenedNativeDirectory(ctx, root, expectedKind, relativePath, func(entry nativeEntry) error {
		return visitNativeDirectoryNames(entry.fd, func(name string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return visit(name)
		})
	})
}

func visitOpenedNativeDirectory(
	ctx context.Context,
	root string,
	expectedKind artifact.ArtifactKind,
	relativePath string,
	visit func(nativeEntry) error,
) (resultErr error) {
	handle, err := openNativeRoot(root, expectedKind)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.close()) }()

	target, err := handle.openRelative(relativePath)
	if err != nil {
		return err
	}
	if target != nil {
		defer func() { resultErr = errors.Join(resultErr, target.close()) }()
	}
	entry := handle.entry
	if target != nil {
		entry = target.entry
	}
	if entry.kind != nativeKindDirectory {
		return fmt.Errorf("artifact access path %q is not a directory", relativePath)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := visit(entry); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if target != nil {
		if err := target.verify(); err != nil {
			return err
		}
	}
	if err := handle.verify(); err != nil {
		return err
	}
	return nil
}

func readFileNative(
	ctx context.Context,
	root string,
	expectedKind artifact.ArtifactKind,
	relativePath string,
	maxBytes int64,
) (result []byte, resultMode fs.FileMode, resultErr error) {
	handle, err := openNativeRoot(root, expectedKind)
	if err != nil {
		return nil, 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.close()) }()

	target, err := handle.openRelative(relativePath)
	if err != nil {
		return nil, 0, err
	}
	if target != nil {
		defer func() { resultErr = errors.Join(resultErr, target.close()) }()
	}
	entry := handle.entry
	if target != nil {
		entry = target.entry
	}
	if entry.kind != nativeKindFile {
		return nil, 0, fmt.Errorf("artifact access path %q is not a regular file", relativePath)
	}
	if entry.size > maxBytes {
		return nil, 0, newLimitError("read", relativePath, maxBytes, entry.size)
	}
	reader := &nativeFileReader{ctx: ctx, fd: entry.fd}
	readLimit := maxBytes
	if maxBytes < math.MaxInt64 {
		readLimit++
	}
	content, err := io.ReadAll(io.LimitReader(reader, readLimit))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(content)) > maxBytes {
		return nil, 0, newLimitError("read", relativePath, maxBytes, int64(len(content)))
	}
	if int64(len(content)) != entry.size {
		return nil, 0, fmt.Errorf("artifact access file %q changed size while reading", relativePath)
	}
	if target != nil {
		if err := target.verify(); err != nil {
			return nil, 0, err
		}
	}
	if err := handle.verify(); err != nil {
		return nil, 0, err
	}
	return content, entry.mode, nil
}

type nativeFileReader struct {
	ctx context.Context
	fd  int
}

func (reader *nativeFileReader) Read(payload []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if len(payload) == 0 {
		return 0, nil
	}
	for {
		count, err := unix.Read(reader.fd, payload)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if count == 0 && err == nil {
			return 0, io.EOF
		}
		return count, err
	}
}
