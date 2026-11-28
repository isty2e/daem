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
	// ErrUnfinishedFinalizationPair identifies a verified pair ineligible for cleanup alone.
	ErrUnfinishedFinalizationPair = fmt.Errorf("%w: the selected files have not both reached their recorded final state", ErrInterruptedFileSetTransaction)
	// ErrFileSetRetirementUnconfirmed reports residue or an indeterminate removal outcome.
	ErrFileSetRetirementUnconfirmed = errors.New("file-set evidence retirement could not be confirmed")
)

// CommittedPair is read-only evidence of one settled generation-3 file set.
// It binds the complete private inventory and both after-image identities.
type CommittedPair struct {
	pairEvidence
}

type pairEvidence struct {
	stateDir, replacedPath, companionPath string
	inventory                             mutationfs.DirectorySnapshot
	markerHash                            string
	images                                [2]pairImage
	settlement                            RecoveryDisposition
	removesCompanion                      bool
}

type pairImage struct {
	path, hash string // An empty hash identifies an absent file.
	identity   storagecommit.EntryIdentity
}

func (pair *CommittedPair) SameEvidence(other *CommittedPair) bool {
	return pair != nil && other != nil && pair.pairEvidence.same(other.pairEvidence)
}

func (pair pairEvidence) same(other pairEvidence) bool {
	return pair.stateDir == other.stateDir &&
		pair.replacedPath == other.replacedPath && pair.companionPath == other.companionPath &&
		pair.inventory.Equal(other.inventory) && pair.markerHash == other.markerHash &&
		pair.settlement == other.settlement && pair.images[0].same(other.images[0]) && pair.images[1].same(other.images[1])
}

func (image pairImage) same(other pairImage) bool {
	return image.path == other.path && image.hash == other.hash &&
		(image.hash == "" || image.identity.SameObject(other.identity))
}

// ReadReplacedImage returns bytes from the exact manifest after-image bound
// by the proof, without following a later link or adopting a replacement.
func (pair *CommittedPair) ReadReplacedImage(ctx context.Context) ([]byte, error) {
	for _, image := range pair.images {
		if image.path != pair.replacedPath {
			continue
		}
		snapshot, err := observeFinalizationFile(ctx, image.path, maximumTargetBytes, &fenceObservationBudget{})
		if err != nil {
			return nil, err
		}
		if !image.matches(snapshot) {
			return nil, mutation.StaleSnapshotError{}
		}
		return snapshot.Content(), nil
	}
	return nil, fmt.Errorf("file-set pair has no replaced image")
}

func (image pairImage) matches(snapshot mutationfs.RegularFileSnapshot) bool {
	return mutationfs.SameEntryObject(image.identity, snapshot.Identity()) && hashBytes(snapshot.Content()) == image.hash
}

// ObserveCommittedPair verifies an exact file set without syncing or removing
// anything. A nil proof with no error means the active directory is absent.
func ObserveCommittedPair(ctx context.Context, stateDir, replacedPath, companionPath string) (*CommittedPair, error) {
	observation, err := observePair(ctx, stateDir, replacedPath, companionPath)
	if err != nil || observation == nil {
		return nil, err
	}
	defer observation.close()
	if observation.evidence.settlement != RecoveryFinalize {
		return nil, ErrUnfinishedFinalizationPair
	}
	return &CommittedPair{pairEvidence: observation.evidence}, nil
}

type pairObservation struct {
	evidence   pairEvidence
	marker     transactionMarker
	capability rootedpath.CommitCapability
	budget     *fenceObservationBudget
}

func (observation *pairObservation) close() {
	if observation.capability != nil {
		_ = observation.capability.Close()
	}
}

func (observation *pairObservation) inventory(ctx context.Context) (mutationfs.DirectorySnapshot, error) {
	if err := admitFileSetFenceObservation(transactionDir(observation.evidence.stateDir), mutationfs.MaximumPhysicalPathDepth, observation.budget, maximumFileSetTargets+1, 0); err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	return storagecommit.SnapshotRootedDirectoryEntries(ctx, observation.capability, maximumFileSetTargets+1)
}

func observePair(ctx context.Context, stateDir, replacedPath, companionPath string) (*pairObservation, error) {
	if ctx == nil {
		return nil, fmt.Errorf("file-set pair observation requires context")
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
		evidence:   pairEvidence{stateDir: stateDir, replacedPath: replacedPath, companionPath: companionPath},
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
	marker, markerHash, err := readPairEvidence(ctx, stateDir, inventory, replacedPath, companionPath, budget)
	if err != nil {
		return nil, err
	}
	images, settlement, err := observePairImages(ctx, marker, budget)
	if err != nil {
		return nil, err
	}
	observation.marker = marker
	observation.evidence.inventory = inventory
	observation.evidence.markerHash = markerHash
	observation.evidence.images = images
	observation.evidence.settlement = settlement
	for index, target := range marker.Targets {
		if target.Path == companionPath {
			observation.evidence.removesCompanion = !target.Before.Exists && images[index].hash != ""
		}
	}
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
	if !pair.pairEvidence.same(observation.evidence) {
		return false, mutation.StaleSnapshotError{}
	}
	if err := observation.confirmFinalImages(ctx, confirm); err != nil {
		return false, err
	}
	if err := revalidate(ctx); err != nil {
		return false, err
	}
	if err := observation.validateFinalImages(ctx); err != nil {
		return false, err
	}
	return observation.retireEvidence(ctx)
}

func (observation *pairObservation) confirmFinalImages(ctx context.Context, confirm func(context.Context, string, storagecommit.EntryIdentity) error) error {
	for _, image := range observation.evidence.images {
		if err := confirm(ctx, image.path, image.identity); err != nil {
			return fmt.Errorf("confirm settled file-set target: %w", err)
		}
	}
	return nil
}

func (observation *pairObservation) validateFinalImages(ctx context.Context) error {
	for _, image := range observation.evidence.images {
		current, err := storagecommit.CaptureEntryIdentity(ctx, image.path)
		if err != nil {
			return err
		}
		if !image.identity.Equal(current) {
			snapshot, err := observeFinalizationFile(ctx, image.path, maximumTargetBytes, observation.budget)
			if err != nil {
				return err
			}
			if !image.matches(snapshot) {
				return mutation.StaleSnapshotError{}
			}
		}
	}
	return nil
}

func (observation *pairObservation) validateEvidence(ctx context.Context) error {
	evidence := observation.evidence
	if err := rejectAbandonedFileSetResidueWithBudget(ctx, evidence.stateDir, mutationfs.MaximumPhysicalPathDepth, observation.budget); err != nil {
		return err
	}
	current, err := observation.inventory(ctx)
	if err != nil {
		return err
	}
	if !evidence.inventory.Equal(current) {
		return mutation.StaleSnapshotError{}
	}
	marker, err := observeFinalizationFile(ctx, markerPath(evidence.stateDir), maximumMarkerBytes, observation.budget)
	if err != nil {
		return err
	}
	if hashBytes(marker.Content()) != evidence.markerHash {
		return mutation.StaleSnapshotError{}
	}
	return nil
}

func (observation *pairObservation) retireEvidence(ctx context.Context) (bool, error) {
	if err := observation.validateEvidence(ctx); err != nil {
		return false, err
	}
	directoryIdentity, ok := observation.evidence.inventory.RootIdentity().(storagecommit.EntryIdentity)
	if !ok {
		return false, fmt.Errorf("file-set evidence directory identity is unavailable")
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

func readPairEvidence(ctx context.Context, stateDir string, inventory mutationfs.DirectorySnapshot, replacedPath, companionPath string, budget *fenceObservationBudget) (transactionMarker, string, error) {
	if !inventory.RootOwnedByInvoker() || inventory.RootMode() != 0o700 {
		return transactionMarker{}, "", wrapFileSetEvidenceInvalid(fmt.Errorf("settlement requires a private, owned evidence directory"))
	}
	entries := make(map[string]mutationfs.DirectoryEntrySnapshot)
	for _, entry := range inventory.Entries() {
		entries[entry.Name()] = entry
	}
	readEvidence := func(path string, maximumBytes int64) ([]byte, error) {
		entry, known := entries[filepath.Base(path)]
		if !known || entry.Kind() != mutationfs.EntryKindFile || !entry.OwnedByInvoker() || (entry.Name() != transactionMarkerFile && entry.Mode() != transactionEvidenceMode) {
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
		return transactionMarker{}, "", wrapFileSetEvidenceInvalid(err)
	}
	marker, err := decodeMarkerVersion(path, content, contractversion.MetadataTransaction, 0, nil)
	if err != nil {
		return transactionMarker{}, "", wrapFileSetEvidenceInvalid(err)
	}
	if !matchesFinalizationPair(marker, replacedPath, companionPath) {
		return transactionMarker{}, "", ErrDifferentFinalizationPair
	}
	for _, target := range marker.Targets {
		if !target.Before.Exists {
			continue
		}
		content, err := readEvidence(target.Before.BackupPath, maximumTargetBytes)
		if err != nil {
			return transactionMarker{}, "", wrapFileSetEvidenceInvalid(err)
		}
		if hashBytes(content) != target.Before.Hash {
			return transactionMarker{}, "", wrapFileSetEvidenceInvalid(fmt.Errorf("file-set backup content differs from its recorded before-image"))
		}
	}
	if len(entries) != 0 {
		return transactionMarker{}, "", wrapFileSetEvidenceInvalid(fmt.Errorf("unexpected entry in file-set evidence directory"))
	}
	return marker, hashBytes(content), nil
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

func observePairImages(ctx context.Context, marker transactionMarker, budget *fenceObservationBudget) ([2]pairImage, RecoveryDisposition, error) {
	var images [2]pairImage
	settlement := RecoveryFinalize
	for index, target := range marker.Targets {
		snapshot, err := observeFinalizationFile(ctx, target.Path, maximumTargetBytes, budget)
		if errors.Is(err, fs.ErrNotExist) {
			if target.Before.Exists {
				return images, "", fmt.Errorf("%w: a recorded file is missing", ErrInterruptedFileSetTransaction)
			}
			images[index] = pairImage{path: target.Path}
			settlement = RecoveryRollback
			continue
		}
		if err != nil {
			return images, "", err
		}
		hash := hashBytes(snapshot.Content())
		before, after := target.positions(true, hash)
		if !after && !before {
			return images, "", fmt.Errorf("%w: a file differs from both recorded states", ErrInterruptedFileSetTransaction)
		}
		identity, ok := snapshot.Identity().(storagecommit.EntryIdentity)
		if !ok {
			return images, "", fmt.Errorf("file-set target identity is unavailable")
		}
		images[index] = pairImage{path: target.Path, hash: hash, identity: identity}
		if !after {
			settlement = RecoveryRollback
		}
	}
	if images[0].identity.SameObject(images[1].identity) {
		return images, "", ErrDifferentFinalizationPair
	}
	return images, settlement, nil
}

func observeFinalizationFile(ctx context.Context, path string, maximumBytes int64, budget *fenceObservationBudget) (mutationfs.RegularFileSnapshot, error) {
	if err := admitFileSetFenceObservation(path, mutationfs.MaximumPhysicalPathDepth, budget, 1, maximumBytes); err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return storagecommit.ReadRegularFileSnapshotUpTo(ctx, path, maximumBytes)
}
