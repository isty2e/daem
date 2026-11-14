package fileset

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

type publicationPlan struct {
	root           string
	intent         PublicationIntent
	limits         mutationfs.TreeTraversalLimits
	rootWitness    *rootedpath.CapturedRoot
	destination    rootedpath.Destination
	activeIdentity storagecommit.EntryIdentity
}

type publicationWorkPurpose uint8

const (
	publicationForwardWork publicationWorkPurpose = iota
	publicationRecoveryWork
)

func bindPublicationPlan(
	ctx context.Context,
	root string,
	intent PublicationIntent,
	headerSize, bodySize int64,
	budget PhysicalWorkBudget,
) (*publicationPlan, error) {
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	if budget == nil || headerSize <= 0 || headerSize > maximumMarkerBytes {
		return nil, fmt.Errorf("publication requires a shared work budget and bounded header")
	}
	if err := validatePublicationIntentSize(bodySize); err != nil {
		return nil, err
	}
	canonicalRoot, err := canonicalStateDirBounded(root, mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	evidenceDirectory := transactionDir(canonicalRoot)
	entries, stagedBytes := 2, headerSize+bodySize
	for _, target := range intent.targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		canonical, err := mutation.CanonicalDirectoryEntryPathBounded(target.path, mutationfs.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, err
		}
		if canonical != target.path {
			return nil, fmt.Errorf("%w: target namespace changed", ErrRecoveryIntentMismatch)
		}
		if target.path == evidenceDirectory || strings.HasPrefix(target.path, evidenceDirectory+string(filepath.Separator)) ||
			strings.HasPrefix(evidenceDirectory, target.path+string(filepath.Separator)) {
			return nil, fmt.Errorf("publication target overlaps its transaction evidence")
		}
		if _, err := storagecommit.NewFileCreate(target.path, nil, target.after.mode); err != nil {
			return nil, err
		}
		if target.before.exists {
			entries++
			stagedBytes += target.before.size
		}
	}
	limits, err := mutationfs.NewTreeTraversalLimits(entries, 0, stagedBytes)
	if err != nil {
		return nil, err
	}
	witness, destination, err := rootedpath.CaptureDestinationBounded(evidenceDirectory, mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	path, err := destination.LexicalPath()
	if err != nil || path != evidenceDirectory {
		_ = witness.Close()
		return nil, fmt.Errorf("%w: evidence namespace changed", ErrRecoveryIntentMismatch)
	}
	return &publicationPlan{
		root: canonicalRoot, intent: intent, limits: limits,
		rootWitness: witness, destination: destination,
	}, nil
}

func (plan *publicationPlan) admitWork(budget PhysicalWorkBudget, purpose publicationWorkPurpose) error {
	if purpose != publicationForwardWork && purpose != publicationRecoveryWork {
		return fmt.Errorf("unknown publication work purpose")
	}
	work := &fenceObservationBudget{}
	add := work.AdmitPhysicalWork
	addSnapshot := func(depth int, size int64) error {
		projection, err := storagecommit.MaximumFileSnapshotWork(depth, size)
		if err != nil {
			return err
		}
		return add(projection.PathWork(), projection.EntryWork(), projection.ByteWork())
	}
	addImage := func(depth int, size int64) error {
		projection, err := publicationEvidenceImageReadWork(depth, size)
		if err != nil {
			return err
		}
		return add(projection.pathComponents, projection.entries, projection.bytes)
	}
	if purpose == publicationForwardWork {
		tree, err := storagecommit.MaximumFlatRootedTreePublicationWork(plan.destination, plan.limits)
		if err != nil {
			return err
		}
		if err := add(tree.PathWork(), tree.EntryWork(), tree.ByteWork()); err != nil {
			return err
		}
	}
	r, err := plan.destination.RootValidationWork()
	if err != nil {
		return err
	}
	// Retirement acquires one capability; optional GC has its own budget.
	acquisitions := 2 * r
	if purpose == publicationForwardWork {
		acquisitions += 2 * r
	}
	if err := add(acquisitions, 0, 0); err != nil {
		return err
	}
	renamePaths, err := storagecommit.MaximumRootedEntryRenamePathWork(plan.destination)
	if err != nil {
		return err
	}
	if err := add(renamePaths, 8, 0); err != nil {
		return err
	}
	markerDepth, err := fileSetAbsolutePathWork(markerPath(plan.root), mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		return err
	}
	directoryDepth, err := fileSetAbsolutePathWork(transactionDir(plan.root), mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		return err
	}
	identityWork, err := storagecommit.MaximumFileIdentityWork(directoryDepth)
	if err != nil {
		return err
	}
	// Settlement and retirement each re-read the complete containing record,
	// bounded by matching directory identities before and after the read.
	for range 2 {
		if err := addSnapshot(markerDepth, maximumMarkerBytes); err != nil {
			return err
		}
		if err := addImage(markerDepth, maximumTargetBytes); err != nil {
			return err
		}
		if err := add(2*identityWork.PathWork(), 2*identityWork.EntryWork(), 0); err != nil {
			return err
		}
	}
	for index, target := range plan.intent.targets {
		d, err := fileSetAbsolutePathWork(target.path, mutationfs.MaximumPhysicalPathDepth)
		if err != nil {
			return err
		}
		b, a := target.before.size, target.after.size
		// Classify with Lstat plus a stable read, then recheck immediately
		// before restoration. All payload ceilings are expected sizes.
		if err := add(d, 1, 0); err != nil {
			return err
		}
		if err := addSnapshot(d, max(b, a)); err != nil {
			return err
		}
		if err := addSnapshot(d, max(b, a)); err != nil {
			return err
		}
		// Terminal verification is separate from classification/restoration.
		// Either outcome then confirms the same present file, or captures and
		// confirms the original absence before any evidence is retired.
		if err := addSnapshot(d, max(b, a)); err != nil {
			return err
		}
		persistence, err := storagecommit.MaximumFilePersistenceWork(d)
		if err != nil {
			return err
		}
		confirmationPaths := persistence.PathWork()
		if !target.before.exists {
			absence, err := storagecommit.MaximumRootedAbsenceConfirmationPathWork(d)
			if err != nil {
				return err
			}
			confirmationPaths = max(confirmationPaths, 3*d+absence)
		}
		if err := add(confirmationPaths, persistence.EntryWork(), 0); err != nil {
			return err
		}
		if purpose == publicationForwardWork {
			if err := addSnapshot(d, b); err != nil {
				return err
			}
		}
		backupDepth, err := fileSetAbsolutePathWork(filepath.Join(transactionDir(plan.root), publicationBackupName(index)), mutationfs.MaximumPhysicalPathDepth)
		if err != nil {
			return err
		}
		if target.before.exists {
			if err := addImage(backupDepth, b); err != nil {
				return err
			}
		}
		if !target.Writes() {
			continue
		}
		addCommit := func(size int64) error {
			file, err := storagecommit.MaximumFileCommitWork(d, size)
			if err != nil {
				return err
			}
			return add(file.PathWork(), file.EntryWork(), file.ByteWork())
		}
		if purpose == publicationForwardWork {
			// Verify the live Before, then materialize from its immutable backup.
			if err := addSnapshot(d, b); err != nil {
				return err
			}
			if target.before.exists {
				if err := addImage(backupDepth, b); err != nil {
					return err
				}
			}
			if err := addCommit(a); err != nil {
				return err
			}
		}
		if target.before.exists {
			if err := addImage(backupDepth, b); err != nil {
				return err
			}
			if err := addCommit(b); err != nil {
				return err
			}
		}
		if !target.before.exists {
			// Bounded destination capture D, acquisition <=2D and retained
			// regular cleanup 8*(2R+P)<=16D; no payload is read by removal.
			if err := add(19*d, 0, 3*a); err != nil {
				return err
			}
		}
	}
	failure, err := plan.retirementFailureWork()
	if err != nil {
		return err
	}
	if err := add(failure.pathComponents, failure.entries, failure.bytes); err != nil {
		return err
	}
	return budget.AdmitPhysicalWork(work.paths, work.entries, work.bytes)
}

// A rename error may have occurred after visibility. Reserve both rooted name
// observations, source-absence sync, and a verified no-replace active rebind
// before any target effect, rather than borrowing optional GC work afterward.
func (plan *publicationPlan) retirementFailureWork() (fileSetPhysicalWork, error) {
	work := &fenceObservationBudget{}
	r, err := plan.destination.RootValidationWork()
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	w, err := plan.destination.ParentChainValidationWork()
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	d, err := fileSetAbsolutePathWork(transactionDir(plan.root), mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	absence, err := storagecommit.MaximumRootedAbsenceConfirmationPathWork(d)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	rename, err := storagecommit.MaximumRootedEntryRenamePathWork(plan.destination)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	if err := work.AdmitPhysicalWork(2*(2*r+absence)+2*r+rename, 8, 0); err != nil {
		return fileSetPhysicalWork{}, err
	}
	for range 2 {
		header, err := storagecommit.MaximumFileSnapshotWork(d+1, maximumMarkerBytes)
		if err != nil {
			return fileSetPhysicalWork{}, err
		}
		body, err := publicationEvidenceImageReadWork(d+1, maximumTargetBytes)
		if err != nil {
			return fileSetPhysicalWork{}, err
		}
		// Two raw identity probes seal the view, and two rooted probes bracket
		// its original-object and backup verification under the retained root.
		if err := work.AdmitPhysicalWork(4*d+8*r+4*w+header.PathWork()+body.pathComponents, 4+header.EntryWork()+body.entries, header.ByteWork()+body.bytes); err != nil {
			return fileSetPhysicalWork{}, err
		}
		for _, target := range plan.intent.targets {
			if target.before.exists {
				image, err := publicationEvidenceImageReadWork(d+1, target.before.size)
				if err != nil {
					return fileSetPhysicalWork{}, err
				}
				if err := work.AdmitPhysicalWork(image.pathComponents, image.entries, image.bytes); err != nil {
					return fileSetPhysicalWork{}, err
				}
			}
		}
	}
	return fileSetPhysicalWork{pathComponents: work.paths, entries: work.entries, bytes: work.bytes}, nil
}

func publicationEvidenceImageReadWork(depth int, size int64) (fileSetPhysicalWork, error) {
	snapshot, err := storagecommit.MaximumFileSnapshotWork(depth, size)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	if depth > int(^uint(0)>>1)-snapshot.PathWork() {
		return fileSetPhysicalWork{}, fmt.Errorf("publication evidence image work overflows")
	}
	// One logical image load retains the existing entry/byte units, but its
	// complete namespace work includes the wrapper's extra Lstat traversal.
	return fileSetPhysicalWork{
		pathComponents: depth + snapshot.PathWork(),
		entries:        snapshot.EntryWork(), bytes: snapshot.ByteWork(),
	}, nil
}

func publicationFileLimits(size int64) mutationfs.TreeTraversalLimits {
	limits, err := mutationfs.NewTreeTraversalLimits(0, 0, size)
	if err != nil {
		panic(err) // All callers supply validated image or marker sizes.
	}
	return limits
}
