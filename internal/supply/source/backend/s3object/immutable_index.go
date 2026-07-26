package s3object

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourcecache "github.com/isty2e/daem/internal/supply/source/cache"
)

const (
	immutableLookupRecordVersion = 1
	immutableLookupRecordName    = "record.json"
	maximumLookupRecordBytes     = 16 * 1024
)

type immutableLookupIdentity struct {
	SourceID  artifact.SourceID
	VersionID string
}

func newImmutableLookupIdentity(
	sourceID artifact.SourceID,
	versionID string,
) (immutableLookupIdentity, bool, error) {
	if sourceID == "" {
		return immutableLookupIdentity{}, false, fmt.Errorf("immutable S3 lookup source id is required")
	}
	if versionID == "" {
		return immutableLookupIdentity{}, false, nil
	}
	return immutableLookupIdentity{SourceID: sourceID, VersionID: versionID}, true, nil
}

func (identity immutableLookupIdentity) key() (sourcecache.Key, error) {
	if identity.SourceID == "" || identity.VersionID == "" {
		return sourcecache.Key{}, fmt.Errorf("immutable S3 lookup identity is incomplete")
	}
	return sourcecache.NewKey("s3-immutable", string(identity.SourceID), identity.VersionID)
}

type immutableLookupRecord struct {
	Version            int                   `json:"version"`
	SourceID           artifact.SourceID     `json:"source_id"`
	RequestedVersionID string                `json:"requested_version_id"`
	ResolvedRef        artifact.ResolvedRef  `json:"resolved_ref"`
	ContentHash        artifact.ContentHash  `json:"content_hash"`
	Kind               artifact.ArtifactKind `json:"kind"`
}

func newImmutableLookupRecord(
	identity immutableLookupIdentity,
	resolved acquisition.Resolution,
) (immutableLookupRecord, error) {
	artifactIdentity := resolved.Identity()
	record := immutableLookupRecord{
		Version:            immutableLookupRecordVersion,
		SourceID:           artifactIdentity.SourceID(),
		RequestedVersionID: identity.VersionID,
		ResolvedRef:        artifactIdentity.ResolvedRef(),
		ContentHash:        artifactIdentity.ContentHash(),
		Kind:               artifactIdentity.Kind(),
	}
	if err := record.validate(identity); err != nil {
		return immutableLookupRecord{}, err
	}
	return record, nil
}

func (record immutableLookupRecord) validate(identity immutableLookupIdentity) error {
	if record.Version != immutableLookupRecordVersion {
		return fmt.Errorf("unsupported immutable S3 lookup record version %d", record.Version)
	}
	if record.SourceID != identity.SourceID {
		return fmt.Errorf("immutable S3 lookup source id does not match request")
	}
	if record.RequestedVersionID != identity.VersionID {
		return fmt.Errorf("immutable S3 lookup VersionId does not match request")
	}
	if record.ResolvedRef == "" {
		return fmt.Errorf("immutable S3 lookup resolved ref is required")
	}
	key, err := cacheKeyForS3Artifact(record.SourceID, record.ResolvedRef, record.ContentHash)
	if err != nil {
		return err
	}
	if _, err := sourcecache.NewEntrySpec(key, "content", record.ContentHash, record.Kind); err != nil {
		return fmt.Errorf("immutable S3 lookup artifact identity: %w", err)
	}
	return nil
}

func encodeImmutableLookupRecord(record immutableLookupRecord) ([]byte, error) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode immutable S3 lookup record: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodeImmutableLookupRecord(content []byte) (immutableLookupRecord, error) {
	if len(content) == 0 {
		return immutableLookupRecord{}, fmt.Errorf("immutable S3 lookup record is empty")
	}
	if len(content) > maximumLookupRecordBytes {
		return immutableLookupRecord{}, fmt.Errorf("immutable S3 lookup record exceeds %d bytes", maximumLookupRecordBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var record immutableLookupRecord
	if err := decoder.Decode(&record); err != nil {
		return immutableLookupRecord{}, fmt.Errorf("decode immutable S3 lookup record: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return immutableLookupRecord{}, fmt.Errorf("immutable S3 lookup record contains trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return immutableLookupRecord{}, fmt.Errorf("decode immutable S3 lookup trailing data: %w", err)
	}
	canonical, err := encodeImmutableLookupRecord(record)
	if err != nil {
		return immutableLookupRecord{}, err
	}
	if !bytes.Equal(content, canonical) {
		return immutableLookupRecord{}, fmt.Errorf("immutable S3 lookup record is not canonical")
	}
	return record, nil
}

type immutableLookupIndex struct {
	root   string
	locker sourcecache.Locker
}

func newImmutableLookupIndex(cacheRoot string) immutableLookupIndex {
	return immutableLookupIndex{
		root:   filepath.Join(cacheRoot, "indexes", "s3-immutable"),
		locker: sourcecache.NewLocker(filepath.Join(cacheRoot, "locks", "s3-immutable")),
	}
}

func (index immutableLookupIndex) acquire(
	ctx context.Context,
	identity immutableLookupIdentity,
) (*sourcecache.Lock, error) {
	key, err := identity.key()
	if err != nil {
		return nil, err
	}
	return index.locker.Acquire(ctx, key)
}

func (index immutableLookupIndex) entryRoot(identity immutableLookupIdentity) (string, error) {
	key, err := identity.key()
	if err != nil {
		return "", err
	}
	return filepath.Join(index.root, key.PathComponent()), nil
}

func (index immutableLookupIndex) read(
	ctx context.Context,
	identity immutableLookupIdentity,
) (immutableLookupRecord, bool, error) {
	key, err := identity.key()
	if err != nil {
		return immutableLookupRecord{}, false, err
	}
	entryRoot, err := index.entryRoot(identity)
	if err != nil {
		return immutableLookupRecord{}, false, err
	}
	spec, err := sourcecache.NewEntrySpec(key, immutableLookupRecordName, "", "")
	if err != nil {
		return immutableLookupRecord{}, false, err
	}
	verifiedFile, found, err := sourcecache.ReadVerifiedFile(ctx, entryRoot, spec, maximumLookupRecordBytes)
	if err != nil || !found {
		return immutableLookupRecord{}, false, err
	}
	if verifiedFile.Mode().Perm() != 0o600 {
		return immutableLookupRecord{}, false, fmt.Errorf(
			"immutable S3 lookup record mode is %04o, want 0600",
			verifiedFile.Mode().Perm(),
		)
	}
	content := verifiedFile.Content()
	record, err := decodeImmutableLookupRecord(content)
	if err != nil {
		return immutableLookupRecord{}, false, err
	}
	if err := record.validate(identity); err != nil {
		return immutableLookupRecord{}, false, err
	}
	return record, true, nil
}

func (index immutableLookupIndex) publish(
	ctx context.Context,
	identity immutableLookupIdentity,
	record immutableLookupRecord,
) error {
	if err := record.validate(identity); err != nil {
		return err
	}
	content, err := encodeImmutableLookupRecord(record)
	if err != nil {
		return err
	}
	key, err := identity.key()
	if err != nil {
		return err
	}
	recordHash := artifact.HashFileContent(content)
	spec, err := sourcecache.NewEntrySpec(key, immutableLookupRecordName, recordHash, artifact.ArtifactKindFile)
	if err != nil {
		return err
	}
	entryRoot, err := index.entryRoot(identity)
	if err != nil {
		return err
	}
	_, err = sourcecache.PublishDirectoryOnce(ctx, entryRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		recordPath := filepath.Join(tempRoot, immutableLookupRecordName)
		if err := os.WriteFile(recordPath, content, 0o600); err != nil {
			return "", "", fmt.Errorf("write immutable S3 lookup record: %w", err)
		}
		if err := os.Chmod(recordPath, 0o600); err != nil {
			return "", "", fmt.Errorf("set immutable S3 lookup record mode: %w", err)
		}
		return recordHash, artifact.ArtifactKindFile, nil
	})
	return err
}

func (index immutableLookupIndex) retire(ctx context.Context, identity immutableLookupIdentity) error {
	entryRoot, err := index.entryRoot(identity)
	if err != nil {
		return err
	}
	return sourcecache.RetireDirectory(ctx, entryRoot)
}
