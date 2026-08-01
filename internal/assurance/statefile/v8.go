package statefile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

const (
	maximumStatefileBytes     int64 = 16 << 20
	maximumStatefileJSONDepth       = 64
	statefileMode                   = os.FileMode(0o600)
)

// Load reads current state or one exact empty retired statefile into canonical durable state.
func Load(ctx context.Context, path string) (durable.Snapshot, error) {
	content, err := readStatefile(ctx, path)
	if err != nil {
		return durable.Snapshot{}, err
	}
	snapshot, err := decodePersisted(content)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("decode statefile %q: %w", path, err)
	}
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("canonicalize statefile authority key: %w", err)
	}
	if err := validateLoadedStateAuthority(snapshot, authority); err != nil {
		return durable.Snapshot{}, fmt.Errorf("decode statefile %q: %w", path, err)
	}
	return snapshot, nil
}

// LoadOptional reads admissible persisted state or returns the canonical empty snapshot.
func LoadOptional(ctx context.Context, path string) (durable.Snapshot, error) {
	snapshot, err := Load(ctx, path)
	if err == nil {
		return snapshot, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return durable.EmptySnapshot(), nil
	}
	return durable.Snapshot{}, fmt.Errorf("read statefile: %w", err)
}

// Decode decodes one strict current state JSON value.
func Decode(content []byte) (durable.Snapshot, error) {
	version, err := statefileDocumentVersion(content)
	if err != nil {
		return durable.Snapshot{}, err
	}
	if version != snapshotVersion {
		return durable.Snapshot{}, unsupportedStatefileVersion(version)
	}
	return decodeCurrent(content)
}

func decodePersisted(content []byte) (durable.Snapshot, error) {
	version, err := statefileDocumentVersion(content)
	if err != nil {
		return durable.Snapshot{}, err
	}
	switch version {
	case snapshotVersion:
		return decodeCurrent(content)
	case retiredStatefileVersion:
		return decodeRetiredStatefile(content)
	default:
		return durable.Snapshot{}, unsupportedStatefileVersion(version)
	}
}

func statefileDocumentVersion(content []byte) (int, error) {
	if int64(len(content)) > maximumStatefileBytes {
		return 0, fmt.Errorf("statefile exceeds %d bytes", maximumStatefileBytes)
	}
	return jsonstrict.ValidateVersionedObject(content, "statefile", maximumStatefileJSONDepth)
}

func decodeCurrent(content []byte) (durable.Snapshot, error) {
	var persisted snapshotDTO
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persisted); err != nil {
		return durable.Snapshot{}, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return durable.Snapshot{}, fmt.Errorf("statefile contains multiple JSON values")
	} else if err != io.EOF {
		return durable.Snapshot{}, err
	}
	if persisted.Version != snapshotVersion {
		return durable.Snapshot{}, unsupportedStatefileVersion(persisted.Version)
	}
	return persisted.canonical()
}

func unsupportedStatefileVersion(version int) error {
	if version > snapshotVersion {
		return fmt.Errorf(
			"unsupported statefile version %d; it was written by a newer daem, so upgrade daem before reading it",
			version,
		)
	}
	return fmt.Errorf(
		"unsupported statefile version %d; use the daem version that wrote it to recover or retire managed state before upgrading",
		version,
	)
}

// Marshal renders one canonical Snapshot as deterministic current JSON.
func Marshal(snapshot durable.Snapshot) ([]byte, error) {
	if err := validateSnapshotForPersistence(snapshot); err != nil {
		return nil, err
	}
	persisted := persistedSnapshot(snapshot)
	content, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximumStatefileBytes {
		return nil, fmt.Errorf("statefile exceeds %d bytes", maximumStatefileBytes)
	}
	return content, nil
}

func readStatefile(ctx context.Context, path string) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("statefile context is required")
	}
	canonicalPath, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return nil, fmt.Errorf("canonicalize statefile path: %w", err)
	}
	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, canonicalPath, maximumStatefileBytes)
	if err != nil {
		return nil, err
	}
	if mode := snapshot.Mode().Perm(); mode != statefileMode {
		return nil, fmt.Errorf(
			"statefile %q permissions are %04o, want %04o",
			path,
			mode,
			statefileMode,
		)
	}
	return snapshot.Content(), nil
}

func validateLoadedStateAuthority(
	snapshot durable.Snapshot,
	authority mutation.PersistedDirectoryEntryAuthority,
) error {
	for index, pending := range snapshot.PendingCarrierInstalls() {
		if !authority.Exact().Equal(pending.Owner().StatefileAuthority()) {
			return fmt.Errorf(
				"pending_carrier_installs[%d] belongs to foreign statefile authority %q with semantics %q",
				index,
				pending.Owner().StatefileKey(),
				pending.Owner().StatefileAuthority().Witness(),
			)
		}
	}
	for index, pending := range snapshot.PendingCarrierRemovals() {
		if !authority.Exact().Equal(pending.Owner().StatefileAuthority()) {
			return fmt.Errorf(
				"pending_carrier_removals[%d] belongs to foreign statefile authority %q with semantics %q",
				index,
				pending.Owner().StatefileKey(),
				pending.Owner().StatefileAuthority().Witness(),
			)
		}
	}
	for index, claim := range snapshot.ManagedCarrierClaims() {
		if !authority.Exact().Equal(claim.Owner().StatefileAuthority()) {
			return fmt.Errorf(
				"managed_carrier_claims[%d] belongs to foreign statefile authority %q with semantics %q",
				index,
				claim.Owner().StatefileKey(),
				claim.Owner().StatefileAuthority().Witness(),
			)
		}
	}
	return nil
}
