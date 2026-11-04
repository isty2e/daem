package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

// Ordinary records come from family admission, never capture completion.
type ordinaryRecord struct {
	record recoveryJournal
}

func decodeOrdinarySnapshot(snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (ordinaryRecord, error) {
	value, err := decodeRecoveryJournalSnapshot(snapshot, path, codec)
	if err != nil {
		return ordinaryRecord{}, err
	}
	return ordinaryRecord{record: value}, nil
}

func (record ordinaryRecord) Validate() error {
	if record.record.Version != recoveryJournalVersion {
		return fmt.Errorf("ordinary journal record is required")
	}
	return nil
}

// OperationID returns the operation identity, or an empty string for a zero value.
func (record ordinaryRecord) OperationID() string {
	return record.record.OperationID
}

func (record ordinaryRecord) AuthorityFingerprint(ctx context.Context, codec durable.SnapshotCodec) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := record.Validate(); err != nil {
		return "", err
	}
	return recoveryJournalAuthorityFingerprint(record.record, codec)
}

func (record ordinaryRecord) Encode(codec durable.SnapshotCodec) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	return marshalRecoveryJournalWithStateContentForPromotion(record.record, codec)
}

func (record ordinaryRecord) PromoteProvisionalAcquire(intent ownership.ProvisionalAcquireIntent, transition ownershipmutation.ClaimTransition, codec durable.SnapshotCodec) (ordinaryRecord, error) {
	if err := record.Validate(); err != nil {
		return ordinaryRecord{}, err
	}
	candidate, err := recoveryJournalAfterPromotion(record.record, intent, transition, codec)
	if err != nil {
		return ordinaryRecord{}, err
	}
	return ordinaryRecord{record: candidate}, nil
}

func (record ordinaryRecord) SameAuthorityFingerprint(other ordinaryRecord, codec durable.SnapshotCodec) bool {
	return record.Validate() == nil && other.Validate() == nil && samePromotedJournal(record.record, other.record, codec)
}

func (record ordinaryRecord) RecoveryPlanningBasis(directory, fingerprint string, selected *[]EntrySelection) (recoveryPlanningBasis, error) {
	if err := record.Validate(); err != nil {
		return recoveryPlanningBasis{}, err
	}
	return record.record.recoveryPlanningBasis(directory, fingerprint, selected)
}
