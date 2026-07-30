package retirement

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

const (
	// RecordFileName is the sole authoritative file in a retirement control.
	RecordFileName = "retirement.json"

	// MaximumRecordBytes bounds one retirement record or interrupted temporary.
	MaximumRecordBytes = 16 << 10

	// DirectoryMode is the exact mode for control, residue, and GC directories.
	DirectoryMode fs.FileMode = 0o700

	// RecordMode is the exact mode for records and admitted record temporaries.
	RecordMode fs.FileMode = 0o600

	maximumRecordJSONDepth = 2
	temporaryRecordPrefix  = ".daem-tmp-"
)

// Phase is the durable cleanup phase recorded by one control.
type Phase string

const (
	PhasePrepared   Phase = "prepared"
	PhaseFinalizing Phase = "finalizing"
)

// Record is the canonical durable correlation for one journal retirement.
type Record struct {
	identity Identity
	phase    Phase
}

// NewRecord constructs a validated retirement record.
func NewRecord(
	operationID string,
	journalAuthorityFingerprint string,
	phase Phase,
) (Record, error) {
	identity, err := NewIdentity(operationID, journalAuthorityFingerprint)
	if err != nil {
		return Record{}, err
	}
	if err := validatePhase(phase); err != nil {
		return Record{}, err
	}
	record := Record{identity: identity, phase: phase}
	if _, err := encodeCanonicalRecord(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func validatePhase(phase Phase) error {
	switch phase {
	case PhasePrepared, PhaseFinalizing:
		return nil
	default:
		return fmt.Errorf("unsupported journal retirement phase %q", phase)
	}
}

// Identity returns immutable journal correlation.
func (record Record) Identity() Identity {
	return record.identity
}

// Phase returns the durable cleanup phase.
func (record Record) Phase() Phase {
	return record.phase
}

// Equal reports whether two records carry the same durable retirement facts.
func (record Record) Equal(other Record) bool {
	return record.valid() && other.valid() &&
		record.identity.equal(other.identity) &&
		record.phase == other.phase
}

// Finalizing returns the idempotent finalizing form of this record.
func (record Record) Finalizing() (Record, error) {
	if !record.valid() {
		return Record{}, fmt.Errorf("journal retirement record is uninitialized")
	}
	record.phase = PhaseFinalizing
	return record, nil
}

func (record Record) valid() bool {
	if !record.identity.valid() || validatePhase(record.phase) != nil {
		return false
	}
	_, err := encodeCanonicalRecord(record)
	return err == nil
}

func (record Record) matchesControlName(name Name) bool {
	return name.kind == NameControl && name.BelongsTo(record.identity)
}

type recordDTO struct {
	Version                     int    `json:"version"`
	Phase                       string `json:"phase"`
	OperationID                 string `json:"operation_id"`
	JournalAuthorityFingerprint string `json:"journal_authority_fingerprint"`
}

// Encode emits the single canonical retirement record representation.
func Encode(record Record) ([]byte, error) {
	if !record.valid() {
		return nil, fmt.Errorf("journal retirement record is uninitialized")
	}
	return encodeCanonicalRecord(record)
}

func encodeCanonicalRecord(record Record) ([]byte, error) {
	content, err := json.MarshalIndent(recordDTO{
		Version:                     currentVersion,
		Phase:                       string(record.phase),
		OperationID:                 record.identity.operationID,
		JournalAuthorityFingerprint: record.identity.journalAuthorityFingerprint,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode journal retirement record: %w", err)
	}
	content = append(content, '\n')
	if len(content) > MaximumRecordBytes {
		return nil, fmt.Errorf("journal retirement record exceeds %d bytes", MaximumRecordBytes)
	}
	return content, nil
}

// Decode validates and canonicalizes one retirement record.
func Decode(content []byte) (Record, error) {
	if len(content) > MaximumRecordBytes {
		return Record{}, fmt.Errorf("journal retirement record exceeds %d bytes", MaximumRecordBytes)
	}
	if err := jsonstrict.Validate(
		content,
		"journal retirement record",
		maximumRecordJSONDepth,
	); err != nil {
		return Record{}, err
	}
	var persisted recordDTO
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persisted); err != nil {
		return Record{}, fmt.Errorf("decode journal retirement record: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return Record{}, fmt.Errorf("journal retirement record contains multiple JSON values")
	} else if err != io.EOF {
		return Record{}, fmt.Errorf("decode journal retirement record trailer: %w", err)
	}
	if persisted.Version != currentVersion {
		return Record{}, fmt.Errorf(
			"unsupported journal retirement record version %d",
			persisted.Version,
		)
	}
	return NewRecord(
		persisted.OperationID,
		persisted.JournalAuthorityFingerprint,
		Phase(persisted.Phase),
	)
}
