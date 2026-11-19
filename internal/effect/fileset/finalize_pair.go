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
	// ErrFileSetRetirementUnconfirmed reports residue or an indeterminate removal outcome.
	ErrFileSetRetirementUnconfirmed = errors.New("file-set evidence retirement could not be confirmed")
)

// CommittedPair is read-only evidence of one settled generation-3 file set.
// It binds the complete private inventory and both after-image identities.
type CommittedPair struct {
	stateDir, replacedPath, companionPath string
	inventory                             mutationfs.DirectorySnapshot
	targets                               [2]string
	identities                            [2]storagecommit.EntryIdentity
}

func (pair *CommittedPair) SameEvidence(other *CommittedPair) bool {
	return pair != nil && other != nil && pair.stateDir == other.stateDir &&
		pair.replacedPath == other.replacedPath && pair.companionPath == other.companionPath &&
		pair.inventory.Equal(other.inventory) && pair.targets == other.targets &&
		pair.identities[0].Equal(other.identities[0]) && pair.identities[1].Equal(other.identities[1])
}

// ReadReplacedImage returns bytes from the exact manifest after-image bound
// by the proof, without following a later link or adopting a replacement.
func (pair *CommittedPair) ReadReplacedImage(ctx context.Context) ([]byte, error) {
	for index, path := range pair.targets {
		if path != pair.replacedPath {
			continue
		}
		snapshot, err := observeFinalizationFile(ctx, path, maximumTargetBytes, &fenceObservationBudget{})
		if err != nil {
			return nil, err
		}
		if !pair.identities[index].Equal(snapshot.Identity()) {
			return nil, mutation.StaleSnapshotError{}
		}
		return snapshot.Content(), nil
	}
	return nil, fmt.Errorf("file-set pair has no replaced image")
}

// ObserveCommittedPair verifies an exact file set without syncing or removing
// anything. A nil proof with no error means the active directory is absent.
func ObserveCommittedPair(ctx context.Context, stateDir, replacedPath, companionPath string) (*CommittedPair, error) {
	observation, err := observePair(ctx, stateDir, replacedPath, companionPath)
	if err != nil || observation == nil {
		return nil, err
	}
	defer observation.close()
	return &observation.proof, nil
}

type pairObservation struct {
	proof      CommittedPair
	capability rootedpath.CommitCapability
	budget     *fenceObservationBudget
}

func (observation *pairObservation) close() {
	if observation.capability != nil {
		_ = observation.capability.Close()
	}
}

func (observation *pairObservation) inventory(ctx context.Context) (mutationfs.DirectorySnapshot, error) {
	if err := admitFileSetFenceObservation(transactionDir(observation.proof.stateDir), mutationfs.MaximumPhysicalPathDepth, observation.budget, maximumFileSetTargets+1, 0); err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	return storagecommit.SnapshotRootedDirectoryEntries(ctx, observation.capability, maximumFileSetTargets+1)
}

func observePair(ctx context.Context, stateDir, replacedPath, companionPath string) (*pairObservation, error) {
	if ctx == nil {
		return nil, fmt.Errorf("file-set finalization requires context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if replacedPath == companionPath || !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir {
		return nil, ErrDifferentFinalizationPair
	}
	budget := &fenceObservationBudget{}
	depth := mutationfs.MaximumPhysicalPathDepth
	if err := rejectAbandonedFileSetResidueWithBudget(ctx, stateDir, depth, budget); err != nil {
		return nil, err
	}

	directory := transactionDir(stateDir)
	root, destination, err := rootedpath.CaptureDestinationBounded(directory, depth, budget)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	path, err := destination.LexicalPath()
	if err != nil || path != directory {
		return nil, mutation.StaleSnapshotError{}
	}
	capability, err := root.AcquireBounded(destination, depth, budget)
	if err != nil {
		return nil, err
	}
	observation := &pairObservation{
		proof:      CommittedPair{stateDir: stateDir, replacedPath: replacedPath, companionPath: companionPath},
		capability: capability,
		budget:     budget,
	}
	keep := false
	defer func() {
		if !keep {
			observation.close()
		}
	}()
	inventory, err := observation.inventory(ctx)
	if errors.Is(err, fs.ErrNotExist) {
		// A missing child in an unstable inventory does not establish that the
		// active record is gone.
		if _, identityErr := storagecommit.CaptureRootedEntryIdentity(ctx, capability); errors.Is(identityErr, fs.ErrNotExist) {
			return nil, nil
		}
	}
	if err != nil {
		return nil, wrapFileSetEvidenceInvalid(err)
	}
	marker, err := readPairFinalizationEvidence(ctx, stateDir, inventory, replacedPath, companionPath, budget)
	if err != nil {
		return nil, err
	}
	identities, err := committedPairIdentities(ctx, marker, budget)
	if err != nil {
		return nil, err
	}
	observation.proof.inventory = inventory
	observation.proof.targets = [2]string{marker.Targets[0].Path, marker.Targets[1].Path}
	observation.proof.identities = identities
	keep = true
	return observation, nil
}

// Retire checks the same evidence before syncing after-images and retiring its
// directory. The caller holds both targets' and the marker's leases.
func (pair *CommittedPair) Retire(ctx context.Context, revalidate func(context.Context) error) (bool, error) {
	return pair.retire(ctx, revalidate, storagecommit.ConfirmFilePersistence)
}

func (pair *CommittedPair) retire(ctx context.Context, revalidate func(context.Context) error, confirm func(context.Context, string, storagecommit.EntryIdentity) error) (bool, error) {
	if pair == nil || revalidate == nil {
		return false, fmt.Errorf("file-set retirement requires evidence and revalidation")
	}
	observation, err := observePair(ctx, pair.stateDir, pair.replacedPath, pair.companionPath)
	if err != nil {
		return false, err
	}
	if observation == nil {
		return false, mutation.StaleSnapshotError{}
	}
	defer observation.close()
	if !pair.SameEvidence(&observation.proof) {
		return false, mutation.StaleSnapshotError{}
	}
	directoryIdentity, ok := pair.inventory.RootIdentity().(storagecommit.EntryIdentity)
	if !ok {
		return false, fmt.Errorf("file-set evidence directory identity is unavailable")
	}
	for index, path := range pair.targets {
		if err := confirm(ctx, path, pair.identities[index]); err != nil {
			return false, fmt.Errorf("confirm settled file-set target: %w", err)
		}
	}

	if err := revalidate(ctx); err != nil {
		return false, err
	}
	if err := rejectAbandonedFileSetResidueWithBudget(ctx, pair.stateDir, mutationfs.MaximumPhysicalPathDepth, observation.budget); err != nil {
		return false, err
	}
	for index, path := range pair.targets {
		current, err := storagecommit.CaptureEntryIdentity(ctx, path)
		if err != nil {
			return false, err
		}
		if !pair.identities[index].Equal(current) {
			return false, mutation.StaleSnapshotError{}
		}
	}
	current, err := observation.inventory(ctx)
	if err != nil {
		return false, err
	}
	if !pair.inventory.Equal(current) {
		return false, mutation.StaleSnapshotError{}
	}

	removal, err := storagecommit.NewRootedLogicalRemoval(observation.capability, directoryIdentity)
	if err != nil {
		return false, err
	}
	observation.capability = nil // The commit now owns the capability.
	outcome, err := storagecommit.CommitLogicalRemovalWithOutcome(ctx, removal)
	if err != nil {
		if outcome.State() == mutationfs.CommitOutcomeIndeterminate || outcome.State() == mutationfs.CommitOutcomeRetainedRecoverable {
			return false, fmt.Errorf("%w: %w", ErrFileSetRetirementUnconfirmed, err)
		}
		return false, err
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
		if errors.Is(err, fs.ErrNotExist) {
			if target.Before.Exists {
				return identities, fmt.Errorf("%w: a recorded file is missing", ErrInterruptedFileSetTransaction)
			}
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
