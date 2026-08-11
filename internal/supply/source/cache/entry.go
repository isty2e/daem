package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/supply/artifact"
)

const (
	completionRecordName    = ".daem-complete"
	completionRecordVersion = 1
	maximumCompletionBytes  = 16 * 1024

	maximumCachedContentEntries = 100_000
	maximumCachedContentDepth   = 64
	maximumCachedContentBytes   = 4 << 30

	cacheEnvelopeEntryOverhead = 2
	cacheEnvelopeDepthOverhead = 1
)

var ErrInvalidEntry = errors.New("invalid cache entry")

func cacheEnvelopeTraversalLimits() mutationfs.TreeTraversalLimits {
	limits, err := mutationfs.NewTreeTraversalLimits(
		maximumCachedContentEntries+cacheEnvelopeEntryOverhead,
		maximumCachedContentDepth+cacheEnvelopeDepthOverhead,
		maximumCachedContentBytes+maximumCompletionBytes,
	)
	if err != nil {
		panic(err)
	}
	return limits
}

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
	if err := record.validateOwnership(spec); err != nil {
		return err
	}
	if err := validateContentIdentity(record.ContentHash, record.Kind); err != nil {
		return err
	}
	if !spec.accepts(record.ContentHash, record.Kind) {
		return fmt.Errorf("completion record content identity does not match expected identity")
	}
	return nil
}

func (record completionRecord) validateOwnership(spec EntrySpec) error {
	if record.Version != completionRecordVersion {
		return fmt.Errorf("unsupported completion record version %d", record.Version)
	}
	if record.Key != spec.key.PathComponent() {
		return fmt.Errorf("completion record cache key does not match requested key")
	}
	if record.ContentPath != spec.contentPath {
		return fmt.Errorf("completion record content path does not match requested path")
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
