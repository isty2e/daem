package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

func reloadNativePublication(ctx context.Context, directory, operationID, fingerprint string, batch nativePublicationContext, after durable.Snapshot, options CaptureOptions) (result CapturedNativeJournal, resultErr error) {
	inventory, err := loadRecoveryRootInventory(ctx, filepath.Dir(directory), inventoryOptions{
		Filesystem: options.Filesystem, StateCodec: options.StateCodec, PhysicalPathBudget: batch.budget, recordDecoder: decodeNativeCaptureInventoryJournal,
	})
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if inventory.decision.State() != retirement.StateActive || inventory.active == nil {
		return CapturedNativeJournal{}, fmt.Errorf("published native journal is not the sole active operation")
	}
	active := inventory.active
	journal, admitted := active.record.(nativeJournalSnapshots)
	if !admitted || journal.record.OperationID != operationID || active.operationDir != directory || active.identity.JournalAuthorityFingerprint() != fingerprint {
		return CapturedNativeJournal{}, fmt.Errorf("published native operation or fingerprint differs from its staged record")
	}
	if !journal.owner.Equal(batch.owner) || !journal.before.Equal(batch.before) || !journal.after.Equal(after) {
		return CapturedNativeJournal{}, fmt.Errorf("published native journal differs from its complete operation context")
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(directory, recovery.MaximumPhysicalPathDepth, batch.budget)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close(), ctx.Err())
		if resultErr != nil {
			result = CapturedNativeJournal{}
		}
	}()
	if err := ValidateActiveJournalRoot(ctx, options.Filesystem, root, batch.budget, active.physicalAuthority); err != nil {
		return CapturedNativeJournal{}, err
	}
	snapshot, err := readNativeCaptureRecord(ctx, root, options.Filesystem, batch.budget)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if recoveryJournalRecordFingerprint(snapshot.Content()) != fingerprint {
		return CapturedNativeJournal{}, fmt.Errorf("published native journal bytes differ from the staged fingerprint")
	}
	if _, err := decodeNativeInventoryJournal(ctx, snapshot, filepath.Join(directory, recoveryJournalFileName), options.StateCodec); err != nil {
		return CapturedNativeJournal{}, err
	}
	qualified, err := journal.loadQualifiedDocuments(ctx, nativeDocumentReadOptions{
		artifacts: root, filesystem: options.Filesystem, budget: batch.budget, limits: batch.limits,
		codecs: options.Codecs, ordinaryCapabilities: batch.ordinaryCapabilities,
	}, batch.roots)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := ValidateActiveJournalRoot(ctx, options.Filesystem, root, batch.budget, active.physicalAuthority); err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := ValidateActiveJournalAuthority(ctx, options.Filesystem, options.OperationAuthority, active.physicalAuthority); err != nil {
		return CapturedNativeJournal{}, err
	}
	if _, _, err := journal.ordinaryBackups(ctx, directory, active.physicalAuthority, options.Filesystem, batch.budget); err != nil {
		return CapturedNativeJournal{}, err
	}
	capture := CaptureResult{OperationID: operationID, Directory: directory, JournalPath: filepath.Join(directory, recoveryJournalFileName), RecordFingerprint: fingerprint}
	if err := validateNativeActive(ctx, capture, active.physicalAuthority, options.Filesystem, options.OperationAuthority, batch.budget); err != nil {
		return CapturedNativeJournal{}, err
	}
	return CapturedNativeJournal{
		capture: capture,
		journal: journal, qualified: qualified, active: active.physicalAuthority, budget: batch.budget, publication: batch, initialized: true,
	}, nil
}

func decodeNativeCaptureInventoryJournal(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (inventoryJournalRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	version, err := jsonstrict.ValidateVersionedObject(snapshot.Content(), "capture recovery inventory journal", maximumRecoveryJournalJSONDepth)
	if err != nil {
		return nil, err
	}
	switch version {
	case recoveryJournalVersion:
		return decodeLegacyInventoryJournal(ctx, snapshot, path, codec)
	case nativeRecoveryJournalVersion:
		return decodeNativeInventoryJournal(ctx, snapshot, path, codec)
	default:
		return nil, unsupportedRecoveryJournalVersion(version)
	}
}

func readNativeCaptureRecord(ctx context.Context, root *rootedpath.CapturedRoot, filesystem mutationfs.RootedReader, budget *recovery.PhysicalWorkBudget) (mutationfs.RegularFileSnapshot, error) {
	maximum := min(maximumRecoveryJournalBytes, budget.RemainingBytes())
	if maximum <= 0 {
		return mutationfs.RegularFileSnapshot{}, fmt.Errorf("native journal reload exceeds the remaining byte allocation")
	}
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	relative, err := rootedpath.NewRelativeDestination(recoveryJournalFileName)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	bound, err := authority.Bind(relative)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	capability, err := root.AcquireBounded(bound, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	content, mode, identity, readErr := filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximum)
	charged := int64(len(content))
	if readErr != nil {
		charged = maximum
	}
	work, workErr := recovery.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if err := errors.Join(readErr, capability.Close(), workErr, ctx.Err()); err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return mutationfs.NewRegularFileSnapshot(content, mode, identity)
}

// ValidateActive checks the selected directory identity and exact published
// record revision. It does not refresh sources, host state, Registry or leases.
func (captured CapturedNativeJournal) ValidateActive(ctx context.Context, filesystem mutationfs.RootedReader, entry *rootedpath.EntryAuthority, budget *recovery.PhysicalWorkBudget) error {
	if err := captured.Validate(); err != nil {
		return err
	}
	if budget != captured.budget {
		return fmt.Errorf("native active validation requires the original allocation")
	}
	return validateNativeActive(ctx, captured.capture, captured.active, filesystem, entry, budget)
}
