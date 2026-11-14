package fileset

import (
	"fmt"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// Selection admits its entire execution envelope before exposing candidates.
// Execution consumes that envelope, not the shared budget a second time.
func (plan *PublicationCleanup) reserve(budget PhysicalWorkBudget, work fileSetPhysicalWork) error {
	if err := budget.AdmitPhysicalWork(work.pathComponents, work.entries, work.bytes); err != nil {
		return err
	}
	plan.executionWork.pathComponents += work.pathComponents
	plan.executionWork.entries += work.entries
	plan.executionWork.bytes += work.bytes
	return nil
}

func (plan *PublicationCleanup) reserveRoot(budget PhysicalWorkBudget) error {
	active, err := fileSetAbsolutePathWork(transactionDir(plan.root), mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		return err
	}
	marker, err := fileSetAbsolutePathWork(markerPath(plan.root), mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		return err
	}
	destination, err := plan.bind("")
	if err != nil {
		return err
	}
	root, err := cleanupSnapshotWork(destination, 0)
	if err != nil {
		return err
	}
	work := &fenceObservationBudget{}
	// FileSet5 observes only its fixed active entry and marker, never siblings.
	// Include its refusal path and the root-only revalidation in this envelope.
	if err := work.AdmitPhysicalWork(active+2*marker, 2, maximumMarkerBytes); err != nil {
		return err
	}
	if err := work.AdmitPhysicalWork(root.pathComponents, root.entries, root.bytes); err != nil {
		return err
	}
	return plan.reserve(budget, fileSetPhysicalWork{pathComponents: work.paths, entries: work.entries, bytes: work.bytes})
}

func (plan *PublicationCleanup) candidateWork(name string, snapshot mutationfs.DirectorySnapshot) (fileSetPhysicalWork, error) {
	work := &fenceObservationBudget{}
	directory, err := plan.bind(name)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	recheck, err := cleanupSnapshotWork(directory, max(1, len(snapshot.Entries())))
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	if err := work.AdmitPhysicalWork(recheck.pathComponents, recheck.entries, recheck.bytes); err != nil {
		return fileSetPhysicalWork{}, err
	}
	for _, entry := range snapshot.Entries() {
		destination, err := plan.bind(filepath.Join(name, entry.Name()))
		if err != nil {
			return fileSetPhysicalWork{}, err
		}
		removal, err := cleanupRemovalWork(destination, entry.Kind(), publicationFileLimits(entry.Size()))
		if err != nil {
			return fileSetPhysicalWork{}, err
		}
		if err := work.AdmitPhysicalWork(removal.pathComponents, removal.entries, removal.bytes); err != nil {
			return fileSetPhysicalWork{}, err
		}
	}
	empty, err := mutationfs.NewTreeTraversalLimits(0, 0, 0)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	removal, err := cleanupRemovalWork(directory, mutationfs.EntryKindDirectory, empty)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	if err := work.AdmitPhysicalWork(removal.pathComponents, removal.entries, removal.bytes); err != nil {
		return fileSetPhysicalWork{}, err
	}
	return fileSetPhysicalWork{pathComponents: work.paths, entries: work.entries, bytes: work.bytes}, nil
}

func cleanupSnapshotWork(destination rootedpath.Destination, maximumEntries int) (fileSetPhysicalWork, error) {
	r, err := destination.RootValidationWork()
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	w, err := destination.ParentChainValidationWork()
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	return fileSetPhysicalWork{pathComponents: 4*r + 4*w, entries: 2 * (maximumEntries + 1)}, nil
}

func cleanupRemovalWork(destination rootedpath.Destination, kind mutationfs.EntryKind, limits mutationfs.TreeTraversalLimits) (fileSetPhysicalWork, error) {
	envelope, err := mutationfs.NewRootedCleanupWorkEnvelope(kind, limits)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	w, err := destination.ParentChainValidationWork()
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	r, err := destination.RootValidationWork()
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	paths, err := envelope.PathWork(w)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	if kind == mutationfs.EntryKindDirectory {
		paths += 4*r + 2*w
	}
	return fileSetPhysicalWork{pathComponents: paths + 2*r, entries: envelope.EntryWork() + 1, bytes: envelope.ByteWork()}, nil
}

type publicationCleanupCursor struct{ remaining fileSetPhysicalWork }

func (cursor *publicationCleanupCursor) AdmitPathComponents(count int) error {
	return cursor.AdmitPhysicalWork(count, 0, 0)
}

func (cursor *publicationCleanupCursor) AdmitPhysicalWork(paths, entries int, bytes int64) error {
	if paths < 0 || entries < 0 || bytes < 0 || paths > cursor.remaining.pathComponents || entries > cursor.remaining.entries || bytes > cursor.remaining.bytes {
		return fmt.Errorf("reserved inactive cleanup work is exhausted")
	}
	cursor.remaining.pathComponents -= paths
	cursor.remaining.entries -= entries
	cursor.remaining.bytes -= bytes
	return nil
}
