package fileset

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

var (
	// ErrDifferentFinalizationPair preserves the published fence without borrowing its paths.
	ErrDifferentFinalizationPair = fmt.Errorf("%w: record does not belong to the selected file pair", ErrInterruptedFileSetTransaction)
	// ErrUnfinishedFinalizationPair identifies a verified before/after pair that still needs its original writer.
	ErrUnfinishedFinalizationPair = fmt.Errorf("%w: the selected files have not both reached their recorded final state", ErrInterruptedFileSetTransaction)
)

// FinalizeCommittedPair retires an exact generation-3 file set without restoring
// either target. The caller holds both targets' and the marker's leases, supplies
// independently bound physical addresses, and revalidates peer authority before
// removal. A false, nil result means the active directory was already absent.
func FinalizeCommittedPair(ctx context.Context, stateDir, replacedPath, companionPath string, revalidate func(context.Context) error) (bool, error) {
	if ctx == nil || revalidate == nil {
		return false, fmt.Errorf("file-set finalization requires context and revalidation")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if replacedPath == companionPath || !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir {
		return false, ErrDifferentFinalizationPair
	}
	budget := &fenceObservationBudget{}
	depth := mutationfs.MaximumPhysicalPathDepth
	if err := rejectAbandonedFileSetResidueWithBudget(ctx, stateDir, depth, budget); err != nil {
		return false, err
	}

	directory := transactionDir(stateDir)
	root, destination, err := rootedpath.CaptureDestinationBounded(directory, depth, budget)
	if err != nil {
		return false, err
	}
	defer root.Close()
	path, err := destination.LexicalPath()
	if err != nil || path != directory {
		return false, mutation.StaleSnapshotError{}
	}
	observeInventory := func(ctx context.Context) (mutationfs.DirectorySnapshot, error) {
		if err := admitFileSetFenceObservation(directory, depth, budget, maximumFileSetTargets+1, 0); err != nil {
			return mutationfs.DirectorySnapshot{}, err
		}
		capability, err := root.Acquire(destination)
		if err != nil {
			return mutationfs.DirectorySnapshot{}, err
		}
		return storagecommit.SnapshotRootedDirectoryEntries(ctx, capability, maximumFileSetTargets+1)
	}
	inventory, err := observeInventory(ctx)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, wrapFileSetEvidenceInvalid(err)
	}
	marker, err := readPairFinalizationEvidence(ctx, stateDir, inventory, replacedPath, companionPath, budget)
	if err != nil {
		return false, err
	}
	// Acquire the removal capability from the same retained root as the inventory.
	removalCapability, err := root.Acquire(destination)
	if err != nil {
		return false, err
	}
	defer func() {
		if removalCapability != nil {
			_ = removalCapability.Close()
		}
	}()
	directoryIdentity, ok := inventory.RootIdentity().(storagecommit.EntryIdentity)
	if !ok {
		return false, fmt.Errorf("file-set evidence directory identity is unavailable")
	}

	identities, err := committedPairIdentities(ctx, marker, budget)
	if err != nil {
		return false, err
	}
	for index, target := range marker.Targets {
		if err := storagecommit.ConfirmFilePersistence(ctx, target.Path, identities[index]); err != nil {
			return false, fmt.Errorf("confirm settled file-set target: %w", err)
		}
	}

	if err := revalidate(ctx); err != nil {
		return false, err
	}
	if err := rejectAbandonedFileSetResidueWithBudget(ctx, stateDir, depth, budget); err != nil {
		return false, err
	}
	for index, target := range marker.Targets {
		current, err := storagecommit.CaptureEntryIdentity(ctx, target.Path)
		if err != nil {
			return false, err
		}
		if !identities[index].Equal(current) {
			return false, mutation.StaleSnapshotError{}
		}
	}
	current, err := observeInventory(ctx)
	if err != nil {
		return false, err
	}
	if !inventory.Equal(current) {
		return false, mutation.StaleSnapshotError{}
	}

	removal, err := storagecommit.NewRootedLogicalRemoval(removalCapability, directoryIdentity)
	if err != nil {
		return false, err
	}
	removalCapability = nil // The commit now owns the capability.
	outcome, err := storagecommit.CommitLogicalRemovalWithOutcome(ctx, removal)
	if err != nil {
		return false, err
	}
	if outcome.State() != mutationfs.CommitOutcomeComplete {
		return false, fmt.Errorf("file-set evidence retirement did not complete")
	}
	return true, nil
}

func readPairFinalizationEvidence(ctx context.Context, stateDir string, inventory mutationfs.DirectorySnapshot, replacedPath, companionPath string, budget *fenceObservationBudget) (transactionMarker, error) {
	if !inventory.RootOwnedByInvoker() || inventory.RootMode() != 0o700 {
		return transactionMarker{}, wrapFileSetEvidenceInvalid(fmt.Errorf("finalization requires a private, owned evidence directory"))
	}
	entries := make(map[string]mutationfs.DirectoryEntrySnapshot)
	for _, entry := range inventory.Entries() {
		entries[entry.Name()] = entry
	}
	readEvidence := func(path string, maximumBytes int64) ([]byte, error) {
		entry, known := entries[filepath.Base(path)]
		if !known || entry.Kind() != mutationfs.EntryKindFile || !entry.OwnedByInvoker() || entry.Mode() != transactionEvidenceMode {
			return nil, fmt.Errorf("missing or unrecognized file-set evidence entry")
		}
		snapshot, err := observeFinalizationFile(ctx, path, maximumBytes, budget)
		if err != nil {
			return nil, err
		}
		if !entry.Identity().Equal(snapshot.Identity()) {
			return nil, mutation.StaleSnapshotError{}
		}
		delete(entries, entry.Name())
		return snapshot.Content(), nil
	}

	path := markerPath(stateDir)
	content, err := readEvidence(path, maximumMarkerBytes)
	if err != nil {
		return transactionMarker{}, wrapFileSetEvidenceInvalid(err)
	}
	marker, err := decodeMarkerVersion(path, content, contractversion.MetadataTransaction, 0, nil)
	if err != nil {
		return transactionMarker{}, wrapFileSetEvidenceInvalid(err)
	}
	if !matchesFinalizationPair(marker, replacedPath, companionPath) {
		return transactionMarker{}, ErrDifferentFinalizationPair
	}
	for _, target := range marker.Targets {
		if !target.Before.Exists {
			continue
		}
		content, err := readEvidence(target.Before.BackupPath, maximumTargetBytes)
		if err != nil {
			return transactionMarker{}, wrapFileSetEvidenceInvalid(err)
		}
		if hashBytes(content) != target.Before.Hash {
			return transactionMarker{}, wrapFileSetEvidenceInvalid(fmt.Errorf("file-set backup content differs from its recorded before-image"))
		}
	}
	if len(entries) != 0 {
		return transactionMarker{}, wrapFileSetEvidenceInvalid(fmt.Errorf("unexpected entry in file-set evidence directory"))
	}
	return marker, nil
}

func matchesFinalizationPair(marker transactionMarker, replacedPath, companionPath string) bool {
	if len(marker.Targets) != 2 {
		return false
	}
	for _, target := range marker.Targets {
		if target.CommitPoint {
			return false
		}
		switch target.Path {
		case replacedPath:
			if !target.Write || !target.Before.Exists {
				return false
			}
		case companionPath:
			if !target.Write && !target.Before.Exists {
				return false
			}
		default:
			return false
		}
	}
	return true // The decoded marker has unique paths.
}

func committedPairIdentities(ctx context.Context, marker transactionMarker, budget *fenceObservationBudget) ([2]storagecommit.EntryIdentity, error) {
	var identities [2]storagecommit.EntryIdentity
	allAfter := true
	for index, target := range marker.Targets {
		snapshot, err := observeFinalizationFile(ctx, target.Path, maximumTargetBytes, budget)
		if errors.Is(err, fs.ErrNotExist) && !target.Before.Exists {
			allAfter = false
			continue
		}
		if err != nil {
			return identities, err
		}
		hash, mode := hashBytes(snapshot.Content()), snapshot.Mode()
		before := target.Before.Exists && hash == target.Before.Hash && mode == fileMode(target.Before)
		after := before
		if target.Write {
			after = hash == target.AfterHash && mode == fileMode(target.Before)
		}
		if !after && !before {
			return identities, fmt.Errorf("%w: a file differs from both recorded states", ErrInterruptedFileSetTransaction)
		}
		identity, ok := snapshot.Identity().(storagecommit.EntryIdentity)
		if !ok {
			return identities, fmt.Errorf("file-set target identity is unavailable")
		}
		identities[index] = identity
		allAfter = allAfter && after
	}
	if !allAfter {
		return identities, ErrUnfinishedFinalizationPair
	}
	if identities[0].SameObject(identities[1]) {
		return identities, ErrDifferentFinalizationPair
	}
	return identities, nil
}

func observeFinalizationFile(ctx context.Context, path string, maximumBytes int64, budget *fenceObservationBudget) (mutationfs.RegularFileSnapshot, error) {
	if err := admitFileSetFenceObservation(path, mutationfs.MaximumPhysicalPathDepth, budget, 1, maximumBytes); err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return storagecommit.ReadRegularFileSnapshotUpTo(ctx, path, maximumBytes)
}
