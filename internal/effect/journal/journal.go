package journal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func recoveryJournalAuthorityFingerprint(
	journal recoveryJournal,
	stateEncoder durable.SnapshotEncoder,
) (string, error) {
	before, after, err := encodeRecoveryJournalSnapshots(
		journal.StatefileBefore,
		journal.StatefileAfter,
		stateEncoder,
	)
	if err != nil {
		return "", fmt.Errorf("fingerprint recovery journal authority: %w", err)
	}
	content, err := encodeRecoveryJournal(journal, before, after)
	if err != nil {
		return "", fmt.Errorf("fingerprint recovery journal authority: %w", err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(content)), nil
}

const (
	maximumRecoveryJournalBytes int64 = 64 << 20
	recoveryJournalMode               = 0o600
	recoveryJournalVersion            = 7

	// MaximumRecoveryBackupFileBytes is the largest single regular file that
	// recovery capture, observation, staging, or execution may admit.
	MaximumRecoveryBackupFileBytes int64 = 128 << 20

	maximumRecoveryJournalJSONDepth = 64
)

type recoveryJournalDTO struct {
	Version               int                            `json:"version"`
	OperationID           string                         `json:"operation_id"`
	Operation             string                         `json:"operation"`
	CreatedAt             string                         `json:"created_at"`
	ProjectRootProvenance *recoveryProjectRootProvenance `json:"project_root_provenance,omitempty"`
	Entries               []recoveryEntry                `json:"entries"`
	StatefileBefore       json.RawMessage                `json:"statefile_before"`
	StatefileAfter        json.RawMessage                `json:"statefile_after"`
	ClaimTransitions      []recoveryClaimTransition      `json:"claim_transitions,omitempty"`
}

func marshalRecoveryJournalWithStateContent(
	journal recoveryJournal,
	statefileBefore []byte,
	statefileAfter []byte,
) ([]byte, error) {
	if err := validateRecoveryJournalEnvelope(journal); err != nil {
		return nil, err
	}
	if err := validateRecoveryJournalRelationships(journal); err != nil {
		return nil, err
	}
	return encodeRecoveryJournal(journal, statefileBefore, statefileAfter)
}

func encodeRecoveryJournalSnapshots(
	beforeSnapshot durable.Snapshot,
	afterSnapshot durable.Snapshot,
	stateEncoder durable.SnapshotEncoder,
) ([]byte, []byte, error) {
	if stateEncoder == nil {
		return nil, nil, fmt.Errorf("recovery journal state codec is required")
	}
	before, err := stateEncoder.Encode(beforeSnapshot)
	if err != nil {
		return nil, nil, fmt.Errorf("recovery journal statefile_before: %w", err)
	}
	before = append([]byte(nil), before...)
	if !json.Valid(before) {
		return nil, nil, fmt.Errorf("recovery journal statefile_before is not valid JSON")
	}
	after, err := stateEncoder.Encode(afterSnapshot)
	if err != nil {
		return nil, nil, fmt.Errorf("recovery journal statefile_after: %w", err)
	}
	after = append([]byte(nil), after...)
	if !json.Valid(after) {
		return nil, nil, fmt.Errorf("recovery journal statefile_after is not valid JSON")
	}
	return before, after, nil
}

func encodeRecoveryJournal(
	journal recoveryJournal,
	statefileBefore []byte,
	statefileAfter []byte,
) ([]byte, error) {
	persisted := recoveryJournalDTO{
		Version:               journal.Version,
		OperationID:           journal.OperationID,
		Operation:             journal.Operation,
		CreatedAt:             journal.CreatedAt,
		ProjectRootProvenance: journal.ProjectRootProvenance,
		Entries:               append([]recoveryEntry(nil), journal.Entries...),
		StatefileBefore:       json.RawMessage(statefileBefore),
		StatefileAfter:        json.RawMessage(statefileAfter),
		ClaimTransitions:      append([]recoveryClaimTransition(nil), journal.ClaimTransitions...),
	}
	sortRecoveryEntries(persisted.Entries)

	content, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximumRecoveryJournalBytes {
		return nil, fmt.Errorf("recovery journal exceeds %d bytes", maximumRecoveryJournalBytes)
	}
	return content, nil
}

func loadRecoveryJournal(
	ctx context.Context,
	filesystem mutationfs.PathReader,
	path string,
	stateCodec durable.SnapshotCodec,
) (recoveryJournal, error) {
	snapshot, err := filesystem.ReadRegularFileSnapshotUpTo(ctx, path, maximumRecoveryJournalBytes)
	if err != nil {
		return recoveryJournal{}, fmt.Errorf("read recovery journal: %w", err)
	}
	if mode := snapshot.Mode().Perm(); mode != recoveryJournalMode {
		return recoveryJournal{}, fmt.Errorf(
			"recovery journal %q permissions are %04o, want %04o",
			path,
			mode,
			recoveryJournalMode,
		)
	}
	content := snapshot.Content()
	if !utf8.Valid(content) {
		return recoveryJournal{}, fmt.Errorf("recovery journal is not valid UTF-8")
	}
	if err := validateRecoveryJournalJSON(content); err != nil {
		return recoveryJournal{}, err
	}

	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		return recoveryJournal{}, err
	}
	if envelope.Version != recoveryJournalVersion {
		return recoveryJournal{}, fmt.Errorf("unsupported recovery journal version %d", envelope.Version)
	}

	var persisted recoveryJournalDTO
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persisted); err != nil {
		return recoveryJournal{}, err
	}
	if stateCodec == nil {
		return recoveryJournal{}, fmt.Errorf("recovery journal state codec is required")
	}
	before, err := stateCodec.Decode(persisted.StatefileBefore)
	if err != nil {
		return recoveryJournal{}, fmt.Errorf("recovery journal statefile_before: %w", err)
	}
	after, err := stateCodec.Decode(persisted.StatefileAfter)
	if err != nil {
		return recoveryJournal{}, fmt.Errorf("recovery journal statefile_after: %w", err)
	}
	journal := recoveryJournal{
		Version:               persisted.Version,
		OperationID:           persisted.OperationID,
		Operation:             persisted.Operation,
		CreatedAt:             persisted.CreatedAt,
		ProjectRootProvenance: persisted.ProjectRootProvenance,
		Entries:               persisted.Entries,
		StatefileBefore:       before,
		StatefileAfter:        after,
		ClaimTransitions:      persisted.ClaimTransitions,
	}
	if err := validateRecoveryJournal(journal, stateCodec); err != nil {
		return recoveryJournal{}, err
	}

	return journal, nil
}

func validateRecoveryJournalJSON(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := consumeRecoveryJournalJSONValue(decoder, 0); err != nil {
		return err
	}
	if token, err := decoder.Token(); err == nil {
		return fmt.Errorf("recovery journal contains multiple JSON values beginning with %v", token)
	} else if err != io.EOF {
		return err
	}
	return nil
}

func consumeRecoveryJournalJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maximumRecoveryJournalJSONDepth {
		return fmt.Errorf(
			"recovery journal JSON exceeds maximum depth %d",
			maximumRecoveryJournalJSONDepth,
		)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("recovery journal object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("recovery journal contains duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeRecoveryJournalJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("recovery journal object has invalid closing delimiter %v", closing)
		}
	case '[':
		for decoder.More() {
			if err := consumeRecoveryJournalJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("recovery journal array has invalid closing delimiter %v", closing)
		}
	default:
		return fmt.Errorf("recovery journal has unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func validateRecoveryJournal(journal recoveryJournal, stateEncoder durable.SnapshotEncoder) error {
	if stateEncoder == nil {
		return fmt.Errorf("recovery journal state codec is required")
	}
	if err := validateRecoveryJournalEnvelope(journal); err != nil {
		return err
	}
	if _, _, err := encodeRecoveryJournalSnapshots(
		journal.StatefileBefore,
		journal.StatefileAfter,
		stateEncoder,
	); err != nil {
		return err
	}
	return validateRecoveryJournalRelationships(journal)
}

func validateRecoveryJournalEnvelope(journal recoveryJournal) error {
	if journal.Version != recoveryJournalVersion {
		return fmt.Errorf("unsupported recovery journal version %d", journal.Version)
	}
	if !isSafeRecoveryOperationID(journal.OperationID) {
		return fmt.Errorf("recovery operation id %q must be a safe path component", journal.OperationID)
	}
	if journal.Operation != recoveryOperationApply {
		return fmt.Errorf("unsupported recovery operation %q", journal.Operation)
	}
	if _, err := time.Parse(time.RFC3339, journal.CreatedAt); err != nil {
		return fmt.Errorf("recovery journal created_at: %w", err)
	}
	if err := validateRecoveryEntries(journal.Entries); err != nil {
		return err
	}
	if err := validateProjectRootProvenanceCoverage(journal); err != nil {
		return err
	}
	return nil
}

func validateRecoveryJournalRelationships(journal recoveryJournal) error {
	claimTransitions, err := canonicalClaimTransitions(journal.ClaimTransitions)
	if err != nil {
		return err
	}
	if len(journal.Entries) == 0 {
		if len(claimTransitions) != 0 {
			return fmt.Errorf("state-only recovery journal must not contain ownership claim transitions")
		}
		if journal.StatefileBefore.Equal(journal.StatefileAfter) {
			return fmt.Errorf("recovery journal requires host entries or a statefile change")
		}
	}
	for index, entry := range journal.Entries {
		if entry.Aggregate != nil && !entry.StateIndependent {
			return fmt.Errorf("recovery entries[%d]: aggregate contract requires state-independent recovery", index)
		}
		if entry.Aggregate != nil {
			contract, err := entry.Aggregate.canonical()
			if err != nil {
				return fmt.Errorf("recovery entries[%d].aggregate: %w", index, err)
			}
			document := contract.Address().Document()
			if entry.Target != string(document.Target()) || entry.Scope != string(document.Scope()) ||
				entry.Path != document.AggregateRoot() || entry.ContentPath != string(contract.Address().ContentPath()) {
				return fmt.Errorf("recovery entries[%d].aggregate: contract differs from path identity", index)
			}
			if _, ok, admissionErr := aggregate.OperationPreconditionsForCodec(contract.CodecContractID()); admissionErr != nil {
				return fmt.Errorf("recovery entries[%d].aggregate: %w", index, admissionErr)
			} else if !ok {
				return fmt.Errorf("recovery entries[%d].aggregate: codec %q is not admitted", index, contract.CodecContractID())
			}
			if entry.StateBefore.Managed || entry.StateExpectedAfter.Managed {
				return fmt.Errorf("recovery entries[%d].aggregate: group path row must not duplicate subject state", index)
			}
		}
		entryIdentity := recoveryStateIdentityFromEntry(entry)
		if entry.ContentKind != "" {
			expectedKind, err := managedPathStateKind(realization.PathProjectionContentKind(entry.ContentKind))
			if err != nil {
				return fmt.Errorf("recovery entries[%d].content_kind: %w", index, err)
			}
			if entry.Before.Existed && entry.Before.Kind != expectedKind {
				return fmt.Errorf("recovery entries[%d].before: managed path requires %s kind", index, expectedKind)
			}
			if entry.ExpectedAfter.Existed && entry.ExpectedAfter.Kind != expectedKind {
				return fmt.Errorf("recovery entries[%d].expected_after: managed path requires %s kind", index, expectedKind)
			}
		}
		if err := validateRecoveryStateExpectedAfter(entry.StateExpectedAfter, fmt.Sprintf("recovery entries[%d].state_expected_after", index)); err != nil {
			return err
		}
		if entry.StateBeforeIdentity != nil && !entry.StateBefore.Managed {
			return fmt.Errorf("recovery entries[%d].state_before_identity: requires managed state_before", index)
		}
		if entry.StateBeforeIdentity != nil {
			if err := validateRecoveryStateIdentity(*entry.StateBeforeIdentity); err != nil {
				return fmt.Errorf("recovery entries[%d].state_before_identity: %w", index, err)
			}
			if sameRecoveryStateIdentity(*entry.StateBeforeIdentity, entryIdentity) {
				return fmt.Errorf("recovery entries[%d].state_before_identity: must differ from the post-action identity", index)
			}
		}
	}
	if err := validateRecoveryJournalStatefile(journal.StatefileBefore, journal.Entries, true); err != nil {
		return err
	}
	if err := validateRecoveryJournalStatefile(journal.StatefileAfter, journal.Entries, false); err != nil {
		return err
	}

	return nil
}

func validateRecoveryStateExpectedAfter(state recoveryManagedMembership, context string) error {
	if err := validateRecoveryManagedMembership(state); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	return nil
}

func managedPathStateKind(contentKind realization.PathProjectionContentKind) (string, error) {
	switch contentKind {
	case realization.PathProjectionFile:
		return recovery.PathKindFile, nil
	case realization.PathProjectionDirectory:
		return recovery.PathKindDirectory, nil
	default:
		return "", fmt.Errorf("managed path content kind %q is not executable", contentKind)
	}
}
