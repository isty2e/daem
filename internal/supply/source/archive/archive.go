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
	materializer := archiveMaterializer{root: root, accounting: &accounting}
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

		if err := accounting.countLogicalEntry(header.Name); err != nil {
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
			if err := materializer.ensureDirectory(targetPath); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := accounting.admitFile(header.Size, name); err != nil {
				return err
			}
			if err := materializer.ensureDirectory(filepath.Dir(targetPath)); err != nil {
				return err
			}
			if err := materializer.admitFile(targetPath); err != nil {
				return err
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
	budget                 budget
	logicalEntryCount      int64
	materializedEntryCount int64
	expandedSize           int64
}

func (accounting *archiveAccounting) countLogicalEntry(name string) error {
	accounting.logicalEntryCount++
	if accounting.logicalEntryCount > accounting.budget.entryCount {
		return newLimitError(LimitEntryCount, accounting.budget.entryCount, accounting.logicalEntryCount, name)
	}
	if int64(len(name)) > accounting.budget.pathBytes {
		return newLimitError(LimitPathBytes, accounting.budget.pathBytes, int64(len(name)), name)
	}
	return nil
}

func (accounting *archiveAccounting) admitMaterializedEntry(name string) error {
	accounting.materializedEntryCount++
	if accounting.materializedEntryCount > accounting.budget.entryCount {
		return newLimitError(LimitEntryCount, accounting.budget.entryCount, accounting.materializedEntryCount, name)
	}
	return nil
}

type archiveMaterializer struct {
	root       string
	accounting *archiveAccounting
}

func (materializer archiveMaterializer) ensureDirectory(directory string) error {
	relative, err := filepath.Rel(materializer.root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("archive directory %q escapes artifact directory", directory)
	}
	if relative == "." {
		return nil
	}

	current := materializer.root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		switch {
		case statErr == nil:
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("archive directory %q is not a directory", current)
			}
		case os.IsNotExist(statErr):
			entryName, relativeErr := filepath.Rel(materializer.root, current)
			if relativeErr != nil {
				return fmt.Errorf("identify archive directory %q: %w", current, relativeErr)
			}
			if err := materializer.accounting.admitMaterializedEntry(filepath.ToSlash(entryName)); err != nil {
				return err
			}
			if err := os.Mkdir(current, 0o700); err != nil {
				return fmt.Errorf("create archive directory %q: %w", current, err)
			}
		default:
			return fmt.Errorf("inspect archive directory %q: %w", current, statErr)
		}
	}
	return nil
}

func (materializer archiveMaterializer) admitFile(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("archive file %q is not a regular file", path)
		}
		return nil
	case os.IsNotExist(err):
		entryName, relativeErr := filepath.Rel(materializer.root, path)
		if relativeErr != nil {
			return fmt.Errorf("identify archive file %q: %w", path, relativeErr)
		}
		return materializer.accounting.admitMaterializedEntry(filepath.ToSlash(entryName))
	default:
		return fmt.Errorf("inspect archive file %q: %w", path, err)
	}
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
