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

func inspectNative(root string) (
	result artifact.ArtifactKind,
	authority nativePathWitness,
	resultErr error,
) {
	handle, err := openNativeRoot(context.Background(), root, "", nativePathWitness{})
	if err != nil {
		return "", nativePathWitness{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.close()) }()
	switch handle.entry.kind {
	case nativeKindFile:
		return artifact.ArtifactKindFile, handle.authority, nil
	case nativeKindDirectory:
		return artifact.ArtifactKindDirectory, handle.authority, nil
	default:
		return "", nativePathWitness{}, fmt.Errorf("artifact access root has unsupported kind")
	}
}

func readDirectoryNative(
	ctx context.Context,
	view View,
	relativePath string,
) ([]Entry, error) {
	entries := make([]Entry, 0)
	if err := visitDirectoryNative(ctx, view, relativePath, func(entry Entry) error {
		entries = append(entries, entry)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(left int, right int) bool {
		return entries[left].name < entries[right].name
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func visitDirectoryNative(
	ctx context.Context,
	view View,
	relativePath string,
	visit func(Entry) error,
) error {
	_, err := visitOpenedNativeDirectory(ctx, view, relativePath, func(entry nativeEntry) error {
		return visitNativeDirectoryNames(entry.fd, func(name string) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			observed, stat, err := observeNativeEntry(entry.fd, name)
			if err != nil {
				return false, err
			}
			return false, visit(Entry{
				name: name,
				kind: publicEntryKind(observed.kind),
				mode: fs.FileMode(stat.Mode & 0o777),
			})
		})
	})
	return err
}

func visitDirectoryNamesNative(
	ctx context.Context,
	view View,
	relativePath string,
	visit func(string) error,
) (DirectoryListingWitness, error) {
	return visitOpenedNativeDirectory(ctx, view, relativePath, func(entry nativeEntry) error {
		return visitNativeDirectoryNames(entry.fd, func(name string) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			return false, visit(name)
		})
	})
}

func verifyDirectoryListingNative(
	ctx context.Context,
	view View,
	relativePath string,
	expected DirectoryListingWitness,
) error {
	current, err := visitOpenedNativeDirectory(ctx, view, relativePath, func(nativeEntry) error {
		return nil
	})
	if err != nil {
		return err
	}
	if !current.identity.equal(expected.identity) {
		return fmt.Errorf("artifact access directory listing %q changed after observation", relativePath)
	}
	return ctx.Err()
}

func visitOpenedNativeDirectory(
	ctx context.Context,
	view View,
	relativePath string,
	visit func(nativeEntry) error,
) (result DirectoryListingWitness, resultErr error) {
	handle, err := openNativeRoot(ctx, view.root, view.kind, view.rootAuthority)
	if err != nil {
		return DirectoryListingWitness{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.close()) }()

	target, err := handle.openRelative(ctx, relativePath)
	if err != nil {
		return DirectoryListingWitness{}, err
	}
	if target != nil {
		defer func() { resultErr = errors.Join(resultErr, target.close()) }()
	}
	entry := handle.entry
	if target != nil {
		entry = target.entry
	}
	if entry.kind != nativeKindDirectory {
		return DirectoryListingWitness{}, fmt.Errorf("artifact access path %q is not a directory", relativePath)
	}
	if err := ctx.Err(); err != nil {
		return DirectoryListingWitness{}, err
	}
	if err := visit(entry); err != nil {
		return DirectoryListingWitness{}, err
	}
	if err := ctx.Err(); err != nil {
		return DirectoryListingWitness{}, err
	}
	relativeAuthority := nativePathWitness{}
	if target != nil {
		if err := target.verify(ctx); err != nil {
			return DirectoryListingWitness{}, err
		}
		relativeAuthority = target.authority
	}
	if err := handle.verify(ctx); err != nil {
		return DirectoryListingWitness{}, err
	}
	if err := ctx.Err(); err != nil {
		return DirectoryListingWitness{}, err
	}
	return newDirectoryListingWitness(entry.identity, relativeAuthority), nil
}

func readFileNative(
	ctx context.Context,
	view View,
	relativePath string,
	maxBytes int64,
) (result []byte, resultMode fs.FileMode, resultErr error) {
	handle, err := openNativeRoot(ctx, view.root, view.kind, view.rootAuthority)
	if err != nil {
		return nil, 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.close()) }()

	target, err := handle.openRelative(ctx, relativePath)
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
		if err := target.verify(ctx); err != nil {
			return nil, 0, err
		}
	}
	if err := handle.verify(ctx); err != nil {
		return nil, 0, err
	}
	if err := ctx.Err(); err != nil {
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
