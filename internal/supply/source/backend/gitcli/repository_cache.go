package gitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/supply/source"
)

const (
	repositoryCacheRecordName    = ".daem-repository"
	repositoryCacheRecordVersion = 1
	maximumRepositoryRecordBytes = 16 * 1024
	repositoryCacheAnchorName    = ".daem-cache-root"
)

type cachedRepository struct {
	path    string
	key     string
	locator source.GitLocator
	format  gitObjectFormat
}

type repositoryCacheRecord struct {
	Version int    `json:"version"`
	Key     string `json:"key"`
	Locator string `json:"locator"`
}

func newRepositoryCacheRecord(repository cachedRepository) repositoryCacheRecord {
	return repositoryCacheRecord{
		Version: repositoryCacheRecordVersion,
		Key:     repository.key,
		Locator: repository.locator.String(),
	}
}

func (record repositoryCacheRecord) validate(repository cachedRepository) error {
	if record.Version != repositoryCacheRecordVersion {
		return fmt.Errorf("unsupported repository cache record version %d", record.Version)
	}
	if record.Key != repository.key {
		return fmt.Errorf("repository cache record key does not match requested source")
	}
	if record.Locator != repository.locator.String() {
		return fmt.Errorf("repository cache record locator does not match requested source")
	}
	return nil
}

func encodeRepositoryCacheRecord(record repositoryCacheRecord) ([]byte, error) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode repository cache record: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodeRepositoryCacheRecord(content []byte) (repositoryCacheRecord, error) {
	if len(content) == 0 {
		return repositoryCacheRecord{}, fmt.Errorf("repository cache record is empty")
	}
	if len(content) > maximumRepositoryRecordBytes {
		return repositoryCacheRecord{}, fmt.Errorf("repository cache record exceeds %d bytes", maximumRepositoryRecordBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var record repositoryCacheRecord
	if err := decoder.Decode(&record); err != nil {
		return repositoryCacheRecord{}, fmt.Errorf("decode repository cache record: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return repositoryCacheRecord{}, fmt.Errorf("repository cache record contains trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return repositoryCacheRecord{}, fmt.Errorf("decode repository cache record trailing data: %w", err)
	}
	return record, nil
}

func (resolver Resolver) captureCacheRoot(ctx context.Context) (*rootedpath.CapturedRoot, error) {
	state, err := resolver.requireState()
	if err != nil {
		return nil, err
	}
	if err := storagecommit.PrepareCommitParent(
		ctx,
		filepath.Join(state.cacheRoot, repositoryCacheAnchorName),
	); err != nil {
		return nil, fmt.Errorf("prepare git source cache root: %w", err)
	}
	root, err := rootedpath.CaptureRootNoFollow(state.cacheRoot)
	if err != nil {
		return nil, fmt.Errorf("capture git source cache root authority: %w", err)
	}
	if err := validateGitCacheNamespaces(ctx, root); err != nil {
		_ = root.Close()
		return nil, err
	}
	return root, nil
}

func validateGitCacheNamespaces(ctx context.Context, root *rootedpath.CapturedRoot) error {
	for _, name := range []string{"repos", "artifacts", "locks", "probes"} {
		authority, err := root.Authority()
		if err != nil {
			return err
		}
		relative, err := rootedpath.NewRelativeDestination(name)
		if err != nil {
			return err
		}
		destination, err := authority.Bind(relative)
		if err != nil {
			return err
		}
		capability, err := root.Acquire(destination)
		if err != nil {
			return err
		}
		identity, err := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
		closeErr := capability.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("validate git cache namespace %q: %w", name, err)
		}
		if identity.Kind() != mutationfs.EntryKindDirectory {
			return fmt.Errorf("git cache namespace %q is not a directory", name)
		}
	}
	return nil
}

func (resolver Resolver) ensureRepositoryCacheEntry(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	repository cachedRepository,
) (bool, error) {
	if root == nil {
		return false, fmt.Errorf("git source cache root authority is required")
	}
	repositoryDestination, err := repositoryCacheDestination(root, repository, "")
	if err != nil {
		return false, err
	}
	capability, err := root.Acquire(repositoryDestination)
	if err != nil {
		return false, err
	}
	identity, err := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
	_ = capability.Close()
	switch {
	case err == nil:
		if identity.Kind() != mutationfs.EntryKindDirectory {
			return false, fmt.Errorf("git repository cache entry is not a directory")
		}
		if err := verifyRepositoryCacheRecord(ctx, root, repository); err != nil {
			return false, err
		}
		return false, nil
	case !errors.Is(err, fs.ErrNotExist):
		return false, fmt.Errorf("inspect git repository cache entry: %w", err)
	}

	capability, err = root.Acquire(repositoryDestination)
	if err != nil {
		return false, err
	}
	recordContent, err := encodeRepositoryCacheRecord(newRepositoryCacheRecord(repository))
	if err != nil {
		_ = capability.Close()
		return false, err
	}
	recordPath, err := mutationfs.NewTreeRelativePath(repositoryCacheRecordName)
	if err != nil {
		_ = capability.Close()
		return false, err
	}
	prepared, err := storagecommit.PrepareRootedTree(
		ctx,
		capability,
		func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.SetRootMode(0o700); err != nil {
				return err
			}
			return writer.WriteFile(recordPath, 0o600, bytes.NewReader(recordContent))
		},
	)
	if err != nil {
		return false, fmt.Errorf("prepare git repository cache entry: %w", err)
	}
	if err := prepared.Commit(ctx); err != nil {
		return false, fmt.Errorf("publish git repository cache entry: %w", err)
	}
	if err := verifyRepositoryCacheRecord(ctx, root, repository); err != nil {
		return false, err
	}
	return true, nil
}

func repositoryCacheDestination(
	root *rootedpath.CapturedRoot,
	repository cachedRepository,
	child string,
) (rootedpath.Destination, error) {
	relativePath := path.Join("repos", repository.key)
	if child != "" {
		relativePath = path.Join(relativePath, child)
	}
	relative, err := rootedpath.NewRelativeDestination(relativePath)
	if err != nil {
		return rootedpath.Destination{}, err
	}
	authority, err := root.Authority()
	if err != nil {
		return rootedpath.Destination{}, err
	}
	return authority.Bind(relative)
}

func verifyRepositoryCacheRecord(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	repository cachedRepository,
) error {
	destination, err := repositoryCacheDestination(root, repository, repositoryCacheRecordName)
	if err != nil {
		return err
	}
	return verifyRepositoryRecordAtDestination(ctx, root, destination, repository)
}

func verifyRepositoryRootRecord(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	repository cachedRepository,
) error {
	relative, err := rootedpath.NewRelativeDestination(repositoryCacheRecordName)
	if err != nil {
		return err
	}
	authority, err := root.Authority()
	if err != nil {
		return err
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		return err
	}
	return verifyRepositoryRecordAtDestination(ctx, root, destination, repository)
}

func verifyRepositoryRecordAtDestination(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	repository cachedRepository,
) error {
	capability, err := root.Acquire(destination)
	if err != nil {
		return err
	}
	content, mode, _, err := storagecommit.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		maximumRepositoryRecordBytes,
	)
	closeErr := capability.Close()
	if err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("read git repository cache authority record: %w", err)
	}
	if mode.Perm() != 0o600 {
		return fmt.Errorf(
			"git repository cache authority record mode is %04o, want 0600",
			mode.Perm(),
		)
	}
	record, err := decodeRepositoryCacheRecord(content)
	if err != nil {
		return fmt.Errorf("validate git repository cache authority record: %w", err)
	}
	canonical, err := encodeRepositoryCacheRecord(record)
	if err != nil {
		return fmt.Errorf("validate git repository cache authority record: %w", err)
	}
	if !bytes.Equal(content, canonical) {
		return fmt.Errorf("git repository cache authority record is not canonical")
	}
	if err := record.validate(repository); err != nil {
		return fmt.Errorf("validate git repository cache authority record: %w", err)
	}
	return nil
}

func repositoryForLocator(resolver Resolver, locator source.GitLocator) cachedRepository {
	return newCachedRepository(resolver, locator, gitObjectFormatSHA1)
}

func newCachedRepository(resolver Resolver, locator source.GitLocator, format gitObjectFormat) cachedRepository {
	locatorValue := locator.String()
	return cachedRepository{
		path:    resolver.repositoryCachePath(locatorValue, format),
		key:     repositoryCacheDirectoryName(locatorValue, format),
		locator: locator,
		format:  format,
	}
}

func validateRepositoryRootMode(root *rootedpath.CapturedRoot) error {
	capability, err := root.AcquireWorkingDirectory()
	if err != nil {
		return err
	}
	defer capability.Close()
	directory, err := capability.OpenDirectory()
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return fmt.Errorf("inspect git repository cache directory: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("git repository cache directory mode is %04o, want private directory mode 0700", info.Mode().Perm())
	}
	return nil
}

func trimSingleGitConfigValue(output string) (string, bool) {
	trimmed := strings.TrimSuffix(output, "\n")
	if trimmed == "" || strings.Contains(trimmed, "\n") || strings.ContainsRune(trimmed, '\r') {
		return "", false
	}
	return trimmed, true
}
