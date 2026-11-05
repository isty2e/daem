package record

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/realization/aggregate"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// A decoded record owns persistence facts, not active-journal or effect authority.
// Snapshot admission is explicitly supplied in both directions; v15 stays closed.
type nativeJournalRecord struct {
	Version                int                                `json:"version"`
	OperationID            string                             `json:"operation_id"`
	Operation              string                             `json:"operation"`
	CreatedAt              string                             `json:"created_at"`
	StateAuthority         recoveryStateAuthority             `json:"state_authority"`
	ManifestRootProvenance recoveryRootProvenance             `json:"manifest_root_provenance"`
	Entries                []recoveryEntry                    `json:"entries"`
	StatefileBefore        json.RawMessage                    `json:"statefile_before"`
	StatefileAfter         json.RawMessage                    `json:"statefile_after"`
	ClaimTransitions       []recoveryClaimTransition          `json:"claim_transitions"`
	ProvisionalAcquires    []recoveryProvisionalAcquireIntent `json:"provisional_acquire_intents"`
	RemovalIntents         []recoveryRemovalIntent            `json:"removal_intents"`
	DocumentGroups         []recoveryDocumentGroup            `json:"document_groups"`
}

type recoveryStateAuthority struct {
	StatefileAuthority pathAuthorityDTO `json:"statefile_authority"`
	ManifestPath       string           `json:"manifest_path"`
}

type Native struct {
	record nativeJournalRecord
	owner  stateauthority.Authority
	before durable.Snapshot
	after  durable.Snapshot
}

func decodeNativeJournalRecord(ctx context.Context, content []byte, codec durable.SnapshotCodec) (Native, error) {
	if ctx == nil {
		return Native{}, fmt.Errorf("native journal context is required")
	}
	if err := ctx.Err(); err != nil {
		return Native{}, err
	}
	if int64(len(content)) > MaximumBytes {
		return Native{}, fmt.Errorf("native journal exceeds the journal byte envelope")
	}
	version, err := jsonstrict.ValidateVersionedObject(content, "native recovery journal", MaximumJSONDepth)
	if err != nil {
		return Native{}, err
	}
	if version != NativeVersion {
		return Native{}, UnsupportedVersion(version)
	}
	if err := requireRecoveryJSONFields(content, "native recovery journal",
		"version", "operation_id", "operation", "created_at", "state_authority",
		"manifest_root_provenance", "entries", "statefile_before", "statefile_after",
		"claim_transitions", "provisional_acquire_intents", "removal_intents", "document_groups"); err != nil {
		return Native{}, err
	}
	var record nativeJournalRecord
	if err := decodeRecoveryJSONStrict(content, &record); err != nil {
		return Native{}, err
	}
	return record.reconstructSnapshots(ctx, codec)
}

func (record nativeJournalRecord) reconstructSnapshots(ctx context.Context, codec durable.SnapshotCodec) (Native, error) {
	if record.Version != NativeVersion || record.Operation != OperationApply || !IsSafeOperationID(record.OperationID) {
		return Native{}, fmt.Errorf("native journal has invalid version, operation or operation identity")
	}
	if _, err := time.Parse(time.RFC3339, record.CreatedAt); err != nil {
		return Native{}, fmt.Errorf("native journal creation time: %w", err)
	}
	owner, err := record.StateAuthority.canonical()
	if err != nil {
		return Native{}, err
	}
	if _, err := canonicalRecoveryRootProvenance(record.ManifestRootProvenance); err != nil {
		return Native{}, err
	}
	if codec == nil {
		return Native{}, ErrStateCodecRequired
	}
	before, err := codec.Decode(record.StatefileBefore)
	if err != nil {
		return Native{}, fmt.Errorf("native journal before Snapshot: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Native{}, err
	}
	after, err := codec.Decode(record.StatefileAfter)
	if err != nil {
		return Native{}, fmt.Errorf("native journal after Snapshot: %w", err)
	}
	if _, _, err := EncodeSnapshots(before, after, codec); err != nil {
		return Native{}, err
	}
	if err := validateRecoveryEntries(record.Entries); err != nil {
		return Native{}, err
	}
	workItems := len(record.Entries) + len(record.ClaimTransitions) + len(record.ProvisionalAcquires) + len(record.RemovalIntents)
	for _, entry := range record.Entries {
		subject, err := entry.Subject.canonical()
		if err != nil {
			return Native{}, err
		}
		if id, backed := topologyprojection.EntityID(subject); backed && id.Kind() == entity.KindHostConfig {
			return Native{}, fmt.Errorf("native whole-file state cannot be encoded as a legacy entry")
		}
		if entry.Aggregate != nil {
			contract, err := entry.Aggregate.canonical()
			if err != nil {
				return Native{}, err
			}
			if _, native := aggregate.HostConfigSyntaxForCodec(contract.CodecContractID()); native {
				return Native{}, fmt.Errorf("native keys cannot be encoded as a legacy entry")
			}
		}
	}
	if len(record.DocumentGroups) == 0 && len(record.Entries) == 0 && before.Equal(after) {
		return Native{}, fmt.Errorf("native journal has no host or state work")
	}
	for index, group := range record.DocumentGroups {
		if err := ctx.Err(); err != nil {
			return Native{}, err
		}
		if err := group.validate(record.ManifestRootProvenance); err != nil {
			return Native{}, fmt.Errorf("document group[%d]: %w", index, err)
		}
		workItems += 1 + len(group.Aliases) + len(group.SelectedSubjects) + len(group.BindingIntents)
		if group.Removal != nil {
			workItems += 1 + len(group.Removal.Demands) + len(group.Removal.States)
		}
		if group.ExclusiveClaims.Before != nil {
			workItems += len(*group.ExclusiveClaims.Before)
		}
		if group.ExclusiveClaims.After != nil {
			workItems += len(*group.ExclusiveClaims.After)
		}
		if group.ExclusiveClaims.Acquires != nil {
			workItems += len(*group.ExclusiveClaims.Acquires)
		}
		if group.PiOrder != nil {
			workItems += len(group.PiOrder.Evidence.Relations) + len(group.PiOrder.Evidence.Constraint.Members)
		}
		if err := validateRecoveryOwnershipWorkBudget(workItems, 0, 0); err != nil {
			return Native{}, err
		}
	}
	if err := record.validateWriterSpellingPartition(ctx); err != nil {
		return Native{}, err
	}
	if err := record.validateRemovalCoverage(); err != nil {
		return Native{}, err
	}
	// The frozen per-entry interpreter can validate ordinary entries alongside
	// native Snapshots, but its state-only no-work rule excludes Pi-only groups.
	legacy := recoveryJournal{
		OperationID: record.OperationID, Entries: record.Entries,
		StatefileBefore: before, StatefileAfter: after,
		ClaimTransitions: record.ClaimTransitions, ProvisionalAcquires: record.ProvisionalAcquires,
		RemovalIntents: record.RemovalIntents,
	}
	if len(record.Entries) != 0 || len(record.DocumentGroups) == 0 {
		if err := validateRecoveryJournalRelationships(legacy); err != nil {
			return Native{}, err
		}
	} else if len(record.ClaimTransitions) != 0 || len(record.ProvisionalAcquires) != 0 || len(record.RemovalIntents) != 0 {
		return Native{}, fmt.Errorf("ordinary metadata requires ordinary entries")
	}
	if err := ctx.Err(); err != nil {
		return Native{}, err
	}
	return Native{record: record, owner: owner, before: before, after: after}, nil
}

func marshalNativeJournalRecord(ctx context.Context, record nativeJournalRecord, before, after durable.Snapshot, codec durable.SnapshotCodec) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("native journal context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	beforeContent, afterContent, err := EncodeSnapshots(before, after, codec)
	if err != nil {
		return nil, err
	}
	record.StatefileBefore, record.StatefileAfter = beforeContent, afterContent
	if _, err := record.reconstructSnapshots(ctx, codec); err != nil {
		return nil, err
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	if int64(len(content)) >= MaximumBytes {
		return nil, fmt.Errorf("native journal including newline exceeds the journal byte envelope")
	}
	content = append(content, '\n')
	if _, err := decodeNativeJournalRecord(ctx, content, codec); err != nil {
		return nil, err
	}
	return content, nil
}

func (record *recoveryStateAuthority) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("state authority destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "statefile_authority", "manifest_path"); err != nil {
		return err
	}
	type wire recoveryStateAuthority
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*record = recoveryStateAuthority(decoded)
	return nil
}

func (record recoveryStateAuthority) canonical() (stateauthority.Authority, error) {
	exact, err := canonicalPathAuthority(record.StatefileAuthority)
	if err != nil {
		return stateauthority.Authority{}, err
	}
	return stateauthority.New(exact, record.ManifestPath)
}
