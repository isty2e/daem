package record

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

// Ordinary records come from family admission, never capture completion.
type Ordinary struct {
	record recoveryJournal
}

func decodeOrdinarySnapshot(snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (Ordinary, error) {
	value, err := decodeRecoveryJournalSnapshot(snapshot, path, codec)
	if err != nil {
		return Ordinary{}, err
	}
	return Ordinary{record: value}, nil
}

func (record Ordinary) Validate() error {
	if record.record.Version != OrdinaryVersion {
		return fmt.Errorf("ordinary journal record is required")
	}
	return nil
}

// OperationID returns the operation identity, or an empty string for a zero value.
func (record Ordinary) OperationID() string {
	return record.record.OperationID
}

func (record Ordinary) AuthorityFingerprint(ctx context.Context, codec durable.SnapshotCodec) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := record.Validate(); err != nil {
		return "", err
	}
	return recoveryJournalAuthorityFingerprint(record.record, codec)
}

func (record Ordinary) Encode(codec durable.SnapshotCodec) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	return marshalRecoveryJournalWithStateContentForPromotion(record.record, codec)
}

func (record Ordinary) PromoteProvisionalAcquire(intent ownership.ProvisionalAcquireIntent, transition ownershipmutation.ClaimTransition, codec durable.SnapshotCodec) (Ordinary, error) {
	if err := record.Validate(); err != nil {
		return Ordinary{}, err
	}
	candidate, err := recoveryJournalAfterPromotion(record.record, intent, transition, codec)
	if err != nil {
		return Ordinary{}, err
	}
	return Ordinary{record: candidate}, nil
}

func (record Ordinary) SameAuthorityFingerprint(other Ordinary, codec durable.SnapshotCodec) bool {
	return record.Validate() == nil && other.Validate() == nil && samePromotedJournal(record.record, other.record, codec)
}

func (record Ordinary) RecoveryPlanningBasis(directory, fingerprint string, selected *[]EntrySelection) (PlanningBasis, error) {
	if err := record.Validate(); err != nil {
		return PlanningBasis{}, err
	}
	return record.record.PlanningBasis(directory, fingerprint, selected)
}
