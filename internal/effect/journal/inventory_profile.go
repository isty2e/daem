package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

// RecoveryInventoryProfile admits journal families through their own codecs.
// It contains no host, statefile, Registry or root acquisition capability.
type RecoveryInventoryProfile struct{ decoder inventoryJournalDecoder }

func NewNativeRecoveryInventoryProfile(legacy, native durable.SnapshotCodec) (RecoveryInventoryProfile, error) {
	if legacy == nil || native == nil {
		return RecoveryInventoryProfile{}, fmt.Errorf("recovery inventory requires both selected family codecs")
	}
	return RecoveryInventoryProfile{decoder: nativeRecoveryInventoryDecoder(legacy, native)}, nil
}

func nativeRecoveryInventoryDecoder(legacy, native durable.SnapshotCodec) inventoryJournalDecoder {
	return func(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, _ durable.SnapshotCodec) (inventoryJournalRecord, error) {
		version, err := jsonstrict.ValidateVersionedObject(snapshot.Content(), "recovery inventory journal", maximumRecoveryJournalJSONDepth)
		if err != nil {
			return nil, err
		}
		switch version {
		case recoveryJournalVersion:
			return decodeLegacyInventoryJournal(ctx, snapshot, path, legacy)
		case nativeRecoveryJournalVersion:
			return decodeNativeInventoryJournal(ctx, snapshot, path, native)
		default:
			return nil, unsupportedRecoveryJournalVersion(version)
		}
	}
}

func PrepareJournalCleanupWithInventoryProfile(ctx context.Context, plan retirement.CleanupPlan, root string, depth int, budget *recovery.PhysicalWorkBudget, filesystem mutationfs.RootedStore, profile RecoveryInventoryProfile) (*RetirementContinuation, error) {
	prepared, err := PrepareJournalCleanup(ctx, plan, root, depth, budget, filesystem)
	if err != nil {
		return nil, err
	}
	prepared.journalDecoder = profile.decoder
	return prepared, nil
}
