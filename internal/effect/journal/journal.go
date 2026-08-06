package journal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

var errRecoveryJournalStateCodecRequired = errors.New("recovery journal state codec is required")

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
	return recoveryJournalRecordFingerprint(content), nil
}

func recoveryJournalRecordFingerprint(content []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(content))
}

const (
	maximumRecoveryJournalBytes int64 = 64 << 20
	recoveryJournalMode               = 0o600
	recoveryJournalVersion            = 11

	// MaximumRecoveryBackupFileBytes is the largest single regular file that
	// recovery capture, observation, staging, or execution may admit.
	MaximumRecoveryBackupFileBytes int64 = 128 << 20

	maximumRecoveryJournalJSONDepth = 64
)

type recoveryJournalDTO struct {
	Version                int                                `json:"version"`
	OperationID            string                             `json:"operation_id"`
	Operation              string                             `json:"operation"`
	CreatedAt              string                             `json:"created_at"`
	ManifestRootProvenance recoveryRootProvenance             `json:"manifest_root_provenance"`
	Entries                []recoveryEntry                    `json:"entries"`
	StatefileBefore        json.RawMessage                    `json:"statefile_before"`
	StatefileAfter         json.RawMessage                    `json:"statefile_after"`
	ClaimTransitions       []recoveryClaimTransition          `json:"claim_transitions,omitempty"`
	ProvisionalAcquires    []recoveryProvisionalAcquireIntent `json:"provisional_acquire_intents,omitempty"`
}

func (persisted *recoveryJournalDTO) UnmarshalJSON(content []byte) error {
	if persisted == nil {
		return fmt.Errorf("recovery journal destination is nil")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return fmt.Errorf("recovery journal must be a JSON object: %w", err)
	}
	if fields == nil {
		return fmt.Errorf("recovery journal must be a JSON object")
	}
	if _, present := fields["entries"]; !present {
		return fmt.Errorf(`recovery journal field "entries" is required`)
	}
	if _, present := fields["manifest_root_provenance"]; !present {
		return fmt.Errorf(`recovery journal field "manifest_root_provenance" is required`)
	}

	type wire recoveryJournalDTO
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*persisted = recoveryJournalDTO(decoded)
	return nil
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
		return nil, nil, errRecoveryJournalStateCodecRequired
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
		Version:                journal.Version,
		OperationID:            journal.OperationID,
		Operation:              journal.Operation,
		CreatedAt:              journal.CreatedAt,
		ManifestRootProvenance: journal.ManifestRootProvenance,
		Entries:                append([]recoveryEntry(nil), journal.Entries...),
		StatefileBefore:        json.RawMessage(statefileBefore),
		StatefileAfter:         json.RawMessage(statefileAfter),
		ClaimTransitions:       append([]recoveryClaimTransition(nil), journal.ClaimTransitions...),
		ProvisionalAcquires:    append([]recoveryProvisionalAcquireIntent(nil), journal.ProvisionalAcquires...),
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
	return decodeRecoveryJournalSnapshot(snapshot, path, stateCodec)
}

func decodeRecoveryJournalSnapshot(
	snapshot mutationfs.RegularFileSnapshot,
	path string,
	stateCodec durable.SnapshotCodec,
) (recoveryJournal, error) {
	if snapshot.Identity() == nil ||
		snapshot.Identity().Kind() != mutationfs.EntryKindFile {
		return recoveryJournal{}, fmt.Errorf("recovery journal snapshot is uninitialized")
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
	version, err := jsonstrict.ValidateVersionedObject(
		content,
		"recovery journal",
		maximumRecoveryJournalJSONDepth,
	)
	if err != nil {
		return recoveryJournal{}, err
	}
	if version != recoveryJournalVersion {
		return recoveryJournal{}, unsupportedRecoveryJournalVersion(version)
	}

	var persisted recoveryJournalDTO
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persisted); err != nil {
		return recoveryJournal{}, err
	}
	if stateCodec == nil {
		return recoveryJournal{}, errRecoveryJournalStateCodecRequired
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
		Version:                persisted.Version,
		OperationID:            persisted.OperationID,
		Operation:              persisted.Operation,
		CreatedAt:              persisted.CreatedAt,
		ManifestRootProvenance: persisted.ManifestRootProvenance,
		Entries:                persisted.Entries,
		StatefileBefore:        before,
		StatefileAfter:         after,
		ClaimTransitions:       persisted.ClaimTransitions,
		ProvisionalAcquires:    persisted.ProvisionalAcquires,
	}
	if err := validateRecoveryJournal(journal, stateCodec); err != nil {
		return recoveryJournal{}, err
	}

	return journal, nil
}

func unsupportedRecoveryJournalVersion(version int) error {
	if version > recoveryJournalVersion {
		return fmt.Errorf(
			"unsupported recovery journal version %d; it was written by a newer daem, so upgrade daem before recovery and never discard it without confirming no interrupted apply remains",
			version,
		)
	}
	if version == 10 {
		return fmt.Errorf(
			"unsupported recovery journal version 10; its durable Linux mount witness may be reusable, so use the daem version that wrote it only after independently establishing that recovery remains in the same boot with no intervening unmount or remount; otherwise preserve the journal and backups for manual analysis and never discard them while interrupted effects may remain",
		)
	}
	return fmt.Errorf(
		"unsupported recovery journal version %d; use the daem version that wrote it to recover before upgrading and never discard it without confirming no interrupted apply remains",
		version,
	)
}

func validateRecoveryJournal(journal recoveryJournal, stateEncoder durable.SnapshotEncoder) error {
	if stateEncoder == nil {
		return errRecoveryJournalStateCodecRequired
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
	if err := validateManifestRootProvenance(journal); err != nil {
		return err
	}
	return nil
}

func validateRecoveryJournalRelationships(journal recoveryJournal) error {
	if err := validateRecoveryOwnershipWorkBudget(
		len(journal.Entries),
		len(journal.ClaimTransitions),
		len(journal.ProvisionalAcquires),
	); err != nil {
		return err
	}
	claimTransitions, err := canonicalClaimTransitions(journal.ClaimTransitions)
	if err != nil {
		return err
	}
	provisionalAcquires, err := canonicalProvisionalAcquireIntents(journal.ProvisionalAcquires)
	if err != nil {
		return err
	}
	for index, transition := range claimTransitions {
		if transition.Kind() != ownershipmutation.TransitionAcquire {
			continue
		}
		prepared, present := transition.Prepared().Get()
		if !present || prepared.OperationID() != journal.OperationID {
			return fmt.Errorf("recovery claim_transitions[%d] operation id differs from journal", index)
		}
	}
	for index, intent := range provisionalAcquires {
		if intent.OperationID() != journal.OperationID {
			return fmt.Errorf("recovery provisional_acquire_intents[%d] operation id differs from journal", index)
		}
	}
	if err := validateRecoveryClaimCoverage(
		journal.Entries,
		claimTransitions,
		provisionalAcquires,
	); err != nil {
		return fmt.Errorf("validate recovery ownership coverage: %w", err)
	}
	if len(journal.Entries) == 0 {
		if len(claimTransitions) != 0 || len(provisionalAcquires) != 0 {
			return fmt.Errorf("state-only recovery journal must not contain ownership mutations")
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
				entry.Path != document.AggregateRoot().String() || entry.ContentPath != string(contract.Address().ContentPath()) {
				return fmt.Errorf("recovery entries[%d].aggregate: contract differs from path identity", index)
			}
			if _, ok, admissionErr := aggregate.OperationPreconditionsForContract(contract); admissionErr != nil {
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
