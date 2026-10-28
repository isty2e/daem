package journal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// ordinaryCaptureSource couples the ordinary builder's record to its private
// backup tree. Its lifetime belongs to stageMixedDocumentJournal, not publication.
type ordinaryCaptureSource struct {
	record       ordinaryCaptureMaterial
	view         access.View
	identity     artifact.ExactIdentity
	work         recovery.ArtifactWork
	filesystem   mutationfs.RootedReader
	capabilities RootedCapabilityResolver
}

type nativeMixedStageOptions struct {
	nativeDocumentStageOptions
	roots NativeDocumentRoots
}

// stageMixedDocumentJournal populates one unpublished tree. Capture options must
// contain only ordinary work; the containing operation still owns live authority.
func (journal nativeJournalSnapshots) stageMixedDocumentJournal(ctx context.Context, paths Paths, capture CaptureOptions, comparisons []DocumentComparison, writer mutationfs.RootedTreeWriter, options nativeMixedStageOptions) (fingerprint string, resultErr error) {
	if ctx == nil || writer == nil || options.budget == nil || options.stateCodec == nil || capture.Filesystem == nil || capture.Resolver == nil {
		return "", fmt.Errorf("mixed journal staging requires context, private writer, codec, shared budget and ordinary capture adapters")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(journal.record.Entries) != 0 || len(journal.record.ClaimTransitions) != 0 || len(journal.record.ProvisionalAcquires) != 0 || len(journal.record.RemovalIntents) != 0 {
		return "", fmt.Errorf("mixed staging requires ordinary facts from the ordinary builder only")
	}
	createdAt, err := time.Parse(time.RFC3339, journal.record.CreatedAt)
	if err != nil || !isSafeRecoveryOperationID(journal.record.OperationID) || journal.record.Operation != recoveryOperationApply {
		return "", fmt.Errorf("mixed journal has invalid operation metadata")
	}
	directory, err := os.MkdirTemp("", "daem-journal-ordinary-")
	if err != nil {
		return "", fmt.Errorf("create ordinary recovery backup source: %w", err)
	}
	defer func() {
		if cleanupErr := removePrivateBuildTree(directory); cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove ordinary recovery backup source: %w", cleanupErr))
		}
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			fingerprint = ""
		}
	}()

	ordinary, err := journal.captureOrdinarySource(ctx, paths, directory, createdAt, capture, options.budget)
	if err != nil {
		return "", err
	}
	return journal.stageWithOrdinarySource(ctx, ordinary, comparisons, writer, options)
}

func (journal nativeJournalSnapshots) captureOrdinarySource(ctx context.Context, paths Paths, directory string, createdAt time.Time, capture CaptureOptions, budget *recovery.PhysicalWorkBudget) (ordinaryCaptureSource, error) {
	if budget == nil {
		return ordinaryCaptureSource{}, fmt.Errorf("native ordinary capture requires the containing allocation")
	}
	record, err := buildOrdinaryCaptureMaterial(ctx, paths, directory, journal.record.OperationID, createdAt, journal.before, journal.after, capture, &ordinaryCaptureWork{budget: budget})
	if err != nil {
		return ordinaryCaptureSource{}, err
	}
	view, err := access.OpenView(directory)
	if err != nil {
		return ordinaryCaptureSource{}, err
	}
	if budget.RemainingEntries() == 0 {
		return ordinaryCaptureSource{}, fmt.Errorf("ordinary source hashing requires an overflow entry allowance")
	}
	maximum, err := recovery.NewArtifactWork(budget.RemainingEntries()-1, budget.RemainingBytes())
	if err != nil {
		return ordinaryCaptureSource{}, err
	}
	traversal, structure, err := ordinarySourceLimits(maximum)
	if err != nil {
		return ordinaryCaptureSource{}, err
	}
	hash, measured, hashErr := view.HashDirectoryWithLimits(ctx, traversal, structure)
	work, err := recovery.NewArtifactWork(measured.DescendantEntries(), measured.RegularFileBytes())
	if err != nil {
		return ordinaryCaptureSource{}, err
	}
	if hashErr != nil {
		return ordinaryCaptureSource{}, errors.Join(hashErr, budget.AdmitTree(budget.RemainingTreeWork()))
	}
	if err := budget.AdmitTree(work); err != nil {
		return ordinaryCaptureSource{}, err
	}
	identity, err := artifact.NewExactIdentity("recovery:ordinary-build", artifact.ResolvedRef(record.OperationID), artifact.ArtifactKindDirectory, hash)
	if err != nil {
		return ordinaryCaptureSource{}, err
	}
	// Revalidation before copying ties these per-backup observations to the same
	// whole-tree identity, including changes between hashing and observation.
	if err := validateOrdinaryCaptureBackups(ctx, directory, view, record.Entries, capture.Filesystem, budget); err != nil {
		return ordinaryCaptureSource{}, err
	}
	return ordinaryCaptureSource{record: record, view: view, identity: identity, work: work, filesystem: capture.Filesystem, capabilities: capture.RootedCapability}, nil
}

func (journal nativeJournalSnapshots) stageWithOrdinarySource(ctx context.Context, ordinary ordinaryCaptureSource, comparisons []DocumentComparison, writer mutationfs.RootedTreeWriter, options nativeMixedStageOptions) (string, error) {
	if ordinary.record.OperationID != journal.record.OperationID || ordinary.record.Operation != journal.record.Operation || ordinary.record.CreatedAt != journal.record.CreatedAt || ordinary.record.ManifestRootProvenance != journal.record.ManifestRootProvenance || !ordinary.record.StatefileBefore.Equal(journal.before) || !ordinary.record.StatefileAfter.Equal(journal.after) {
		return "", fmt.Errorf("ordinary backup source does not match the native operation and complete Snapshots")
	}
	record := journal.record
	record.Entries = append([]recoveryEntry{}, ordinary.record.Entries...)
	record.ClaimTransitions = append([]recoveryClaimTransition{}, ordinary.record.ClaimTransitions...)
	record.ProvisionalAcquires = append([]recoveryProvisionalAcquireIntent{}, ordinary.record.ProvisionalAcquires...)
	record.RemovalIntents = append([]recoveryRemovalIntent{}, ordinary.record.RemovalIntents...)
	journal.record = record
	if _, err := journal.qualifyDocumentNamespaces(ctx, nativeDocumentReadOptions{filesystem: ordinary.filesystem, budget: options.budget, ordinaryCapabilities: ordinary.capabilities}, options.roots); err != nil {
		return "", err
	}
	// Each traversal may enumerate one overflow entry when refusing source growth.
	allowance, err := recovery.NewArtifactWork(ordinary.work.Entries()+1, ordinary.work.Bytes())
	if err != nil {
		return "", err
	}
	work, err := recovery.NewArtifactWork(2*allowance.Entries(), 2*allowance.Bytes())
	if err != nil {
		return "", err
	}
	prepared, content, err := journal.prepareDocumentJournal(ctx, comparisons, options.nativeDocumentStageOptions, work)
	if err != nil {
		return "", err
	}
	traversal, structure, err := ordinarySourceLimits(ordinary.work)
	if err != nil {
		return "", err
	}
	_, measureErr := ordinary.view.MeasureVerifiedDirectory(ctx, ordinary.identity, traversal, structure)
	measuredWork := ordinary.work
	if measureErr != nil {
		measuredWork = allowance
	}
	if err := errors.Join(measureErr, options.budget.AdmitTree(measuredWork), ctx.Err()); err != nil {
		return "", err
	}
	sink, err := artifactstage.New(writer)
	if err != nil {
		return "", err
	}
	copyErr := ordinary.view.CopyVerifiedWithLimits(ctx, ordinary.identity, sink, traversal, structure)
	copiedWork := ordinary.work
	if copyErr != nil {
		copiedWork = allowance
	}
	if err := errors.Join(copyErr, options.budget.AdmitTree(copiedWork), ctx.Err()); err != nil {
		return "", err
	}
	return prepared.stageJournal(ctx, content, writer, options.budget)
}

func ordinarySourceLimits(work recovery.ArtifactWork) (access.TraversalLimit, access.TreeStructureLimit, error) {
	traversal, err := access.NewTraversalLimit(uint64(work.Entries())+1, work.Bytes())
	if err != nil {
		return access.TraversalLimit{}, access.TreeStructureLimit{}, err
	}
	// files/ and the backup root wrap each independently bounded host tree.
	structure, err := access.NewTreeStructureLimit(work.Entries(), recovery.MaximumArtifactTreeDepth+2)
	return traversal, structure, err
}
