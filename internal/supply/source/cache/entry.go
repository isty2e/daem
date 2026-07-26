package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

const (
	completionRecordName    = ".daem-complete"
	completionRecordVersion = 1
	maximumCompletionBytes  = 16 * 1024
)

var ErrInvalidEntry = errors.New("invalid cache entry")

// EntrySpec identifies one cache entry and any caller-known content identity.
type EntrySpec struct {
	key          Key
	contentPath  string
	expectedHash artifact.ContentHash
	expectedKind artifact.ArtifactKind
}

// VerifiedFile is an owned snapshot of one file-backed cache payload whose
// containing entry and completion record were verified against the same bytes.
type VerifiedFile struct {
	content []byte
	mode    fs.FileMode
}

// Content returns an owned copy of the verified payload bytes.
func (file VerifiedFile) Content() []byte {
	return slices.Clone(file.content)
}

// Mode returns the permission bits observed with the payload snapshot.
func (file VerifiedFile) Mode() fs.FileMode {
	return file.mode
}

// NewEntrySpec constructs a cache entry specification. Expected hash and kind
// must either both be supplied or both be omitted.
func NewEntrySpec(
	key Key,
	contentPath string,
	expectedHash artifact.ContentHash,
	expectedKind artifact.ArtifactKind,
) (EntrySpec, error) {
	if err := key.validate(); err != nil {
		return EntrySpec{}, err
	}
	normalizedPath, err := normalizeContentPath(contentPath)
	if err != nil {
		return EntrySpec{}, err
	}
	if expectedHash != "" || expectedKind != "" {
		if err := validateContentIdentity(expectedHash, expectedKind); err != nil {
			return EntrySpec{}, fmt.Errorf("expected cache content identity: %w", err)
		}
	}
	return EntrySpec{
		key:          key,
		contentPath:  normalizedPath,
		expectedHash: expectedHash,
		expectedKind: expectedKind,
	}, nil
}

func (spec EntrySpec) validate() error {
	if err := spec.key.validate(); err != nil {
		return err
	}
	normalizedPath, err := normalizeContentPath(spec.contentPath)
	if err != nil {
		return err
	}
	if normalizedPath != spec.contentPath {
		return fmt.Errorf("cache content path is not normalized")
	}
	if spec.expectedHash != "" || spec.expectedKind != "" {
		return validateContentIdentity(spec.expectedHash, spec.expectedKind)
	}
	return nil
}

func (spec EntrySpec) accepts(hash artifact.ContentHash, kind artifact.ArtifactKind) bool {
	return spec.expectedHash == "" || (spec.expectedHash == hash && spec.expectedKind == kind)
}

type completionRecord struct {
	Version     int                   `json:"version"`
	Key         string                `json:"key"`
	ContentPath string                `json:"content_path"`
	ContentHash artifact.ContentHash  `json:"content_hash"`
	Kind        artifact.ArtifactKind `json:"kind"`
}

func newCompletionRecord(
	spec EntrySpec,
	hash artifact.ContentHash,
	kind artifact.ArtifactKind,
) (completionRecord, error) {
	if err := spec.validate(); err != nil {
		return completionRecord{}, err
	}
	if err := validateContentIdentity(hash, kind); err != nil {
		return completionRecord{}, err
	}
	if !spec.accepts(hash, kind) {
		return completionRecord{}, fmt.Errorf(
			"built cache content identity %q/%q does not match expected %q/%q",
			hash,
			kind,
			spec.expectedHash,
			spec.expectedKind,
		)
	}
	return completionRecord{
		Version:     completionRecordVersion,
		Key:         spec.key.PathComponent(),
		ContentPath: spec.contentPath,
		ContentHash: hash,
		Kind:        kind,
	}, nil
}

func (record completionRecord) validate(spec EntrySpec) error {
	if record.Version != completionRecordVersion {
		return fmt.Errorf("unsupported completion record version %d", record.Version)
	}
	if record.Key != spec.key.PathComponent() {
		return fmt.Errorf("completion record cache key does not match requested key")
	}
	if record.ContentPath != spec.contentPath {
		return fmt.Errorf("completion record content path does not match requested path")
	}
	if err := validateContentIdentity(record.ContentHash, record.Kind); err != nil {
		return err
	}
	if !spec.accepts(record.ContentHash, record.Kind) {
		return fmt.Errorf("completion record content identity does not match expected identity")
	}
	return nil
}

func encodeCompletionRecord(record completionRecord) ([]byte, error) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode cache completion record: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodeCompletionRecord(content []byte) (completionRecord, error) {
	if len(content) == 0 {
		return completionRecord{}, fmt.Errorf("completion record is empty")
	}
	if len(content) > maximumCompletionBytes {
		return completionRecord{}, fmt.Errorf("completion record exceeds %d bytes", maximumCompletionBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var record completionRecord
	if err := decoder.Decode(&record); err != nil {
		return completionRecord{}, fmt.Errorf("decode completion record: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return completionRecord{}, fmt.Errorf("completion record contains trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return completionRecord{}, fmt.Errorf("decode completion record trailing data: %w", err)
	}
	return record, nil
}

// VerifyDirectory reports whether root is a complete cache entry matching spec.
// Missing roots return false without error; present invalid roots return
// ErrInvalidEntry. Callers that use the result to authorize reuse must hold the
// exact entry lock through the resulting operation.
func VerifyDirectory(ctx context.Context, root string, spec EntrySpec) (bool, error) {
	if err := validateContext(ctx, "entry verification"); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if root == "" {
		return false, fmt.Errorf("cache entry root is required")
	}
	if err := spec.validate(); err != nil {
		return false, err
	}
	root, err := canonicalCacheEntryPath(root)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat cache entry root %q: %w", root, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, invalidEntry(root, "entry root is not a non-symlink directory")
	}

	recordPath := filepath.Join(root, completionRecordName)
	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, recordPath, maximumCompletionBytes)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, contextErr
		}
		return false, invalidEntry(root, fmt.Sprintf("read completion record: %v", err))
	}
	content := snapshot.Content()
	mode := snapshot.Mode()
	if mode.Perm() != 0o600 {
		return false, invalidEntry(root, fmt.Sprintf("completion record mode is %04o, want 0600", mode.Perm()))
	}
	record, err := decodeCompletionRecord(content)
	if err != nil {
		return false, invalidEntry(root, err.Error())
	}
	canonical, err := encodeCompletionRecord(record)
	if err != nil {
		return false, invalidEntry(root, err.Error())
	}
	if !bytes.Equal(content, canonical) {
		return false, invalidEntry(root, "completion record is not canonical")
	}
	if err := record.validate(spec); err != nil {
		return false, invalidEntry(root, err.Error())
	}

	contentPath, err := nonSymlinkContentPath(root, spec.contentPath)
	if err != nil {
		return false, invalidEntry(root, err.Error())
	}
	hash, kind, err := access.HashPath(ctx, contentPath)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, contextErr
		}
		return false, invalidEntry(root, fmt.Sprintf("hash cached content: %v", err))
	}
	if hash != record.ContentHash || kind != record.Kind {
		return false, invalidEntry(root, fmt.Sprintf(
			"cached content identity %q/%q does not match completion record %q/%q",
			hash,
			kind,
			record.ContentHash,
			record.Kind,
		))
	}
	return true, nil
}

// ReadVerifiedFile returns one bounded, no-follow file snapshot only when the
// containing cache entry verifies against the snapshot's exact content hash.
// Missing entries return found=false without error.
func ReadVerifiedFile(
	ctx context.Context,
	root string,
	spec EntrySpec,
	maximumBytes int,
) (VerifiedFile, bool, error) {
	if err := validateContext(ctx, "verified file read"); err != nil {
		return VerifiedFile{}, false, err
	}
	if maximumBytes <= 0 {
		return VerifiedFile{}, false, fmt.Errorf("verified cache file maximum bytes must be positive")
	}
	if err := spec.validate(); err != nil {
		return VerifiedFile{}, false, err
	}
	canonicalRoot, err := canonicalCacheEntryPath(root)
	if err != nil {
		return VerifiedFile{}, false, err
	}
	contentPath := filepath.Join(canonicalRoot, filepath.FromSlash(spec.contentPath))
	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, contentPath, int64(maximumBytes))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return VerifiedFile{}, false, contextErr
		}
		if errors.Is(err, fs.ErrNotExist) {
			return VerifiedFile{}, false, nil
		}
		kind, classified := mutationfs.FailureKindOf(err)
		if classified && kind == mutationfs.FailureUnsupportedGuarantee {
			return VerifiedFile{}, false, err
		}
		return VerifiedFile{}, false, invalidEntry(canonicalRoot, fmt.Sprintf("read cache file: %v", err))
	}
	content := snapshot.Content()
	if len(content) > maximumBytes {
		return VerifiedFile{}, false, invalidEntry(canonicalRoot, fmt.Sprintf(
			"cache file %q exceeds %d bytes",
			spec.contentPath,
			maximumBytes,
		))
	}
	hash := artifact.HashFileContent(content)
	if !spec.accepts(hash, artifact.ArtifactKindFile) {
		return VerifiedFile{}, false, invalidEntry(canonicalRoot, "cache file identity does not match expected identity")
	}
	verifiedSpec := spec
	verifiedSpec.expectedHash = hash
	verifiedSpec.expectedKind = artifact.ArtifactKindFile
	valid, err := VerifyDirectory(ctx, canonicalRoot, verifiedSpec)
	if err != nil || !valid {
		return VerifiedFile{}, false, err
	}
	return VerifiedFile{
		content: slices.Clone(content),
		mode:    snapshot.Mode().Perm(),
	}, true, nil
}

func nonSymlinkContentPath(root string, relativePath string) (string, error) {
	current := root
	for component := range strings.SplitSeq(relativePath, "/") {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("inspect cache content path %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("cache content path component %q is a symlink", current)
		}
	}
	return current, nil
}

func canonicalCacheEntryPath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve cache entry path %q: %w", value, err)
	}
	absolute = filepath.Clean(absolute)
	parent := filepath.Dir(absolute)
	missing := make([]string, 0)
	for {
		_, err := os.Lstat(parent)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", fmt.Errorf("resolve cache entry parent %q: %w", parent, err)
			}
			resolvedInfo, err := os.Lstat(resolved)
			if err != nil {
				return "", fmt.Errorf("inspect resolved cache entry parent %q: %w", resolved, err)
			}
			if !resolvedInfo.IsDir() {
				return "", fmt.Errorf("cache entry ancestor %q is not a directory", parent)
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Join(resolved, filepath.Base(absolute)), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect cache entry ancestor %q: %w", parent, err)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", fmt.Errorf("cache entry path %q has no existing ancestor", value)
		}
		missing = append(missing, filepath.Base(parent))
		parent = next
	}
}

func normalizeContentPath(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("cache content path is required")
	}
	if strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("cache content path %q is not a portable relative path", value)
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "", fmt.Errorf("cache content path %q must stay below the entry root", value)
	}
	if cleaned != value {
		return "", fmt.Errorf("cache content path %q must be normalized", value)
	}
	return cleaned, nil
}

func validateContentIdentity(hash artifact.ContentHash, kind artifact.ArtifactKind) error {
	if strings.TrimSpace(string(hash)) == "" {
		return fmt.Errorf("cache content hash is required")
	}
	if strings.TrimSpace(string(hash)) != string(hash) {
		return fmt.Errorf("cache content hash contains surrounding whitespace")
	}
	switch kind {
	case artifact.ArtifactKindFile, artifact.ArtifactKindDirectory:
		return nil
	default:
		return fmt.Errorf("unsupported cache artifact kind %q", kind)
	}
}

func invalidEntry(root string, reason string) error {
	return fmt.Errorf("%w at %q: %s", ErrInvalidEntry, root, reason)
}
