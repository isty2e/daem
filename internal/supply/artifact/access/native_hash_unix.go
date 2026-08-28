//go:build darwin || linux

package access

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"

	"github.com/isty2e/daem/internal/supply/artifact"
	"golang.org/x/sys/unix"
)

func walkNative(
	ctx context.Context,
	view View,
	sink TreeSink,
	budget *traversalBudget,
) (result artifact.ContentHash, resultErr error) {
	handle, err := openNativeRoot(ctx, view.root, view.kind, view.rootAuthority)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.close()) }()

	rootSize := int64(0)
	if handle.entry.kind == nativeKindFile {
		rootSize = handle.entry.size
	}
	if err := budget.consumeRoot(rootSize); err != nil {
		return "", err
	}
	var contentHash artifact.ContentHash
	switch handle.entry.kind {
	case nativeKindFile:
		contentHash, err = hashNativeFile(ctx, handle.entry, ".", sink)
	case nativeKindDirectory:
		contentHash, err = hashNativeDirectory(ctx, handle.entry, sink, budget)
	default:
		err = fmt.Errorf("artifact access root has unsupported kind")
	}
	if err != nil {
		return "", err
	}
	if err := handle.verify(ctx); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return contentHash, nil
}

func hashNativeFile(
	ctx context.Context,
	entry nativeEntry,
	relativePath string,
	sink TreeSink,
) (artifact.ContentHash, error) {
	reader := &nativeFileReader{ctx: ctx, fd: entry.fd}
	var writer io.WriteCloser
	var content io.Reader = reader
	if sink != nil {
		opened, err := openSinkFile(sink, relativePath, entry.mode, entry.size)
		if err != nil {
			return "", err
		}
		writer = opened
		content = io.TeeReader(reader, io.MultiWriter(writer))
	}
	contentHash, hashErr := artifact.HashFileReader(
		ctx,
		content,
		entry.size,
		entry.mode.Perm()&0o111 != 0,
	)
	if writer != nil {
		hashErr = errors.Join(hashErr, writer.Close())
	}
	if hashErr != nil {
		return "", hashErr
	}
	return contentHash, nil
}

func hashNativeDirectory(
	ctx context.Context,
	root nativeEntry,
	sink TreeSink,
	budget *traversalBudget,
) (artifact.ContentHash, error) {
	if sink != nil {
		if err := sink.BeginDirectory(".", root.mode); err != nil {
			return "", err
		}
	}
	builder := artifact.NewDirectoryHashBuilder()
	if err := hashNativeDirectoryEntries(ctx, root.fd, ".", 0, builder, sink, budget); err != nil {
		return "", err
	}
	if sink != nil {
		if err := sink.EndDirectory(".", root.mode); err != nil {
			return "", err
		}
	}
	return builder.Sum()
}

func hashNativeDirectoryEntries(
	ctx context.Context,
	directoryFD int,
	relativeRoot string,
	directoryDepth int,
	builder *artifact.DirectoryHashBuilder,
	sink TreeSink,
	budget *traversalBudget,
) error {
	names, err := readNativeDirectoryNamesWithinBudget(ctx, directoryFD, relativeRoot, budget)
	if err != nil {
		return err
	}
	if err := requireRootRegularFile(ctx, directoryFD, relativeRoot, names, budget); err != nil {
		return err
	}
	for index, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		relativePath := name
		if relativeRoot != "." {
			relativePath = path.Join(relativeRoot, name)
		}
		entry, err := openNativeChild(directoryFD, name)
		if err != nil {
			if errors.Is(err, ErrUnsupportedSymlink) {
				chargeClassifiedSymlinkRemainder(budget, names, index, true)
				return &unsupportedSymlinkError{path: relativePath}
			}
			return err
		}

		var operationErr error
		entrySize := int64(0)
		if entry.kind == nativeKindFile {
			entrySize = entry.size
		}
		if operationErr = budget.consumeEntry(
			relativePath,
			entrySize,
			entry.kind == nativeKindDirectory,
			directoryDepth,
		); operationErr != nil {
			closeErr := unix.Close(entry.fd)
			return errors.Join(operationErr, closeErr)
		}
		switch entry.kind {
		case nativeKindDirectory:
			operationErr = builder.AddDirectory(relativePath)
			if operationErr == nil && sink != nil {
				operationErr = sink.BeginDirectory(relativePath, entry.mode)
			}
			if operationErr == nil {
				operationErr = hashNativeDirectoryEntries(
					ctx,
					entry.fd,
					relativePath,
					directoryDepth+1,
					builder,
					sink,
					budget,
				)
			}
			if operationErr == nil {
				operationErr = verifyNativeEntryBinding(directoryFD, name, entry)
			}
			if operationErr == nil && sink != nil {
				operationErr = sink.EndDirectory(relativePath, entry.mode)
			}
		case nativeKindFile:
			reader := &nativeFileReader{ctx: ctx, fd: entry.fd}
			var writer io.WriteCloser
			var content io.Reader = reader
			if sink != nil {
				writer, operationErr = openSinkFile(sink, relativePath, entry.mode, entry.size)
				if operationErr == nil {
					content = io.TeeReader(reader, io.MultiWriter(writer))
				}
			}
			if operationErr == nil {
				operationErr = builder.AddFile(
					ctx,
					relativePath,
					entry.mode.Perm()&0o111 != 0,
					entry.size,
					content,
				)
			}
			if writer != nil {
				operationErr = errors.Join(operationErr, writer.Close())
			}
			if operationErr == nil {
				operationErr = verifyNativeEntryBinding(directoryFD, name, entry)
			}
		default:
			operationErr = fmt.Errorf("artifact access path %q has unsupported kind", relativePath)
		}
		closeErr := unix.Close(entry.fd)
		if operationErr != nil {
			if errors.Is(operationErr, ErrUnsupportedSymlink) {
				chargeClassifiedSymlinkRemainder(budget, names, index, false)
			}
			return errors.Join(operationErr, closeErr)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return verifyNativeDirectoryNames(ctx, directoryFD, names)
}

func chargeClassifiedSymlinkRemainder(
	budget *traversalBudget,
	names []string,
	current int,
	includeCurrent bool,
) {
	remaining := len(names) - current
	if !includeCurrent {
		remaining--
	}
	budget.chargeRootListing(remaining)
}

func requireRootRegularFile(
	ctx context.Context,
	directoryFD int,
	relativeRoot string,
	names []string,
	budget *traversalBudget,
) error {
	if budget == nil || budget.requiredRootRegularFile == "" || relativeRoot != "." {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name := budget.requiredRootRegularFile
	if !slices.Contains(names, name) {
		budget.chargeRootListing(len(names))
		return fmt.Errorf("%w: %q", ErrRequiredRootRegularFile, name)
	}
	observed, _, err := observeNativeEntry(directoryFD, name)
	if err != nil {
		budget.chargeRootListing(len(names))
		return err
	}
	if observed.kind != nativeKindFile {
		budget.chargeRootListing(len(names))
		return fmt.Errorf("%w: %q", ErrRequiredRootRegularFile, name)
	}
	return nil
}

func openSinkFile(
	sink TreeSink,
	relativePath string,
	mode fs.FileMode,
	size int64,
) (io.WriteCloser, error) {
	writer, err := sink.OpenFile(relativePath, mode, size)
	if err != nil {
		return nil, err
	}
	if writer == nil {
		return nil, fmt.Errorf("artifact access tree sink returned no writer for %q", relativePath)
	}
	return writer, nil
}
