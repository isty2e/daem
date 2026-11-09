package recoveryoperation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

// RecoveryInventoryProfile admits journal families through their own codecs.
// It contains no host, statefile, Registry or root acquisition capability.
type RecoveryInventoryProfile struct {
	decoder journalinventory.JournalDecoder
}

func NewNativeRecoveryInventoryProfile(legacy, native durable.SnapshotCodec) (RecoveryInventoryProfile, error) {
	if legacy == nil || native == nil {
		return RecoveryInventoryProfile{}, fmt.Errorf("recovery inventory requires both selected family codecs")
	}
	return RecoveryInventoryProfile{decoder: nativeRecoveryInventoryDecoder(legacy, native)}, nil
}

func nativeRecoveryInventoryDecoder(legacy, native durable.SnapshotCodec) journalinventory.JournalDecoder {
	return func(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, _ durable.SnapshotCodec) (journalrecord.Loaded, error) {
		version, err := jsonstrict.ValidateVersionedObject(snapshot.Content(), "recovery inventory journal", journalrecord.MaximumJSONDepth)
		if err != nil {
			return journalrecord.Loaded{}, err
		}
		switch version {
		case journalrecord.OrdinaryVersion:
			return journalrecord.DecodeOrdinarySnapshot(ctx, snapshot, path, legacy)
		case journalrecord.NativeVersion:
			return journalrecord.DecodeNativeSnapshot(ctx, snapshot, path, native)
		default:
			if journalrecord.SupportsNativeVersion(version) {
				return journalrecord.DecodeNativeSnapshot(ctx, snapshot, path, native)
			}
			return journalrecord.Loaded{}, journalrecord.UnsupportedVersion(version)
		}
	}
}

func PrepareJournalCleanupWithInventoryProfile(ctx context.Context, plan retirement.CleanupPlan, root string, depth int, budget *physicalwork.Budget, filesystem mutationfs.RootedStore, profile RecoveryInventoryProfile) (*RetirementContinuation, error) {
	prepared, err := PrepareJournalCleanup(ctx, plan, root, depth, budget, filesystem)
	if err != nil {
		return nil, err
	}
	prepared.journalDecoder = profile.decoder
	return prepared, nil
}
