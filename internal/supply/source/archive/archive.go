package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

// ExtractTar safely extracts a tar archive into outputRoot.
func ExtractTar(ctx context.Context, input io.Reader, outputRoot string) error {
	return extractTarWithBudget(ctx, input, outputRoot, defaultBudget())
}

func extractTarWithBudget(ctx context.Context, input io.Reader, outputRoot string, budget budget) error {
	if err := validateExtraction(ctx, input, budget); err != nil {
		return err
	}
	return extractTar(ctx, newBoundedReader(ctx, input, LimitInputBytes, budget.inputBytes), outputRoot, budget)
}

func extractTar(ctx context.Context, input io.Reader, outputRoot string, budget budget) error {
	if outputRoot == "" {
		return fmt.Errorf("archive output root is required")
	}
	root := filepath.Clean(outputRoot)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create archive output directory %q: %w", root, err)
	}

	reader := tar.NewReader(input)
	accounting := archiveAccounting{budget: budget}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}

		if err := accounting.countEntry(header.Name); err != nil {
			return err
		}
		name, err := cleanArchiveName(header.Name)
		if err != nil {
			return err
		}
		if err := accounting.admitPath(name); err != nil {
			return err
		}

		targetPath := filepath.Join(root, filepath.FromSlash(name))
		if !isWithinDirectory(root, targetPath) {
			return fmt.Errorf("archive entry %q escapes artifact directory", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o700); err != nil {
				return fmt.Errorf("create archive directory %q: %w", targetPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := accounting.admitFile(header.Size, name); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
				return fmt.Errorf("create archive parent directory %q: %w", filepath.Dir(targetPath), err)
			}

			if err := writeArchiveFile(ctx, reader, targetPath, os.FileMode(header.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("archive entry %q is a link; links are not supported", header.Name)
		default:
			return fmt.Errorf("archive entry %q has unsupported type %d", header.Name, header.Typeflag)
		}
	}

	return nil
}

// ExtractTarGzip safely extracts a gzip-compressed tar archive into outputRoot.
func ExtractTarGzip(ctx context.Context, input io.Reader, outputRoot string) error {
	return extractTarGzipWithBudget(ctx, input, outputRoot, defaultBudget())
}

func extractTarGzipWithBudget(ctx context.Context, input io.Reader, outputRoot string, budget budget) error {
	if err := validateExtraction(ctx, input, budget); err != nil {
		return err
	}
	compressed := newBoundedReader(ctx, input, LimitInputBytes, budget.inputBytes)
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer reader.Close()

	stream := newBoundedReader(ctx, reader, LimitTarStreamBytes, budget.tarStreamBytes)
	return extractTar(ctx, stream, outputRoot, budget)
}

func cleanArchiveName(name string) (string, error) {
	if strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") || hasParentSegment(name) {
		return "", fmt.Errorf("archive entry %q is not a safe relative path", name)
	}

	cleanName := pathpkg.Clean(name)
	if cleanName == "." || strings.HasPrefix(cleanName, "../") || cleanName == ".." || pathpkg.IsAbs(cleanName) {
		return "", fmt.Errorf("archive entry %q is not a safe relative path", name)
	}

	return cleanName, nil
}

func hasParentSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}

	return false
}

func writeArchiveFile(ctx context.Context, reader io.Reader, path string, mode os.FileMode) (returnErr error) {
	fileMode := os.FileMode(0o600)
	if mode&0o111 != 0 {
		fileMode = 0o700
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("create archive file %q: %w", path, err)
	}

	defer func() {
		if err := file.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close archive file %q: %w", path, err))
		}
	}()

	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			written, writeErr := file.Write(buffer[:count])
			if writeErr != nil {
				return fmt.Errorf("write archive file %q: %w", path, writeErr)
			}
			if written != count {
				return fmt.Errorf("write archive file %q: %w", path, io.ErrShortWrite)
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			return fmt.Errorf("read archive file %q: %w", path, readErr)
		}
	}
}

type archiveAccounting struct {
	budget       budget
	entryCount   int64
	expandedSize int64
}

func (accounting *archiveAccounting) countEntry(name string) error {
	accounting.entryCount++
	if accounting.entryCount > accounting.budget.entryCount {
		return newLimitError(LimitEntryCount, accounting.budget.entryCount, accounting.entryCount, name)
	}
	if int64(len(name)) > accounting.budget.pathBytes {
		return newLimitError(LimitPathBytes, accounting.budget.pathBytes, int64(len(name)), name)
	}
	return nil
}

func (accounting *archiveAccounting) admitPath(name string) error {
	depth := int64(strings.Count(name, "/") + 1)
	if depth > accounting.budget.pathDepth {
		return newLimitError(LimitPathDepth, accounting.budget.pathDepth, depth, name)
	}
	return nil
}

func (accounting *archiveAccounting) admitFile(size int64, name string) error {
	if size < 0 {
		return fmt.Errorf("archive entry %q has negative size", name)
	}
	if size > accounting.budget.entryBytes {
		return newLimitError(LimitEntryBytes, accounting.budget.entryBytes, size, name)
	}
	if size > math.MaxInt64-accounting.expandedSize {
		return newLimitError(LimitExpandedBytes, accounting.budget.expandedBytes, math.MaxInt64, name)
	}
	observed := accounting.expandedSize + size
	if observed > accounting.budget.expandedBytes {
		return newLimitError(LimitExpandedBytes, accounting.budget.expandedBytes, observed, name)
	}
	accounting.expandedSize = observed
	return nil
}

func validateExtraction(ctx context.Context, input io.Reader, budget budget) error {
	if ctx == nil {
		return fmt.Errorf("archive extraction context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if input == nil {
		return fmt.Errorf("archive input is required")
	}
	return budget.validate()
}

func isWithinDirectory(root string, path string) bool {
	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return relativePath == "." || (!strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) && relativePath != "..")
}
