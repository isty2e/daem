package fileset

import (
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestPublicationEvidenceImageWorkIncludesMetadataProbeAndSnapshot(t *testing.T) {
	for _, size := range []int64{0, 17} {
		work, err := publicationEvidenceImageReadWork(5, size)
		if err != nil || work.pathComponents != 15 || work.entries != 1 || work.bytes != size {
			t.Fatal("evidence loading lost its Lstat, snapshot traversal or payload work", work, err)
		}
	}
	for _, input := range []struct {
		depth int
		size  int64
	}{{depth: 0}, {depth: -1}, {depth: 1, size: -1}, {depth: int(^uint(0)>>1)/3 + 1}} {
		if work, err := publicationEvidenceImageReadWork(input.depth, input.size); err == nil || work != (fileSetPhysicalWork{}) {
			t.Fatal("invalid evidence projection accepted", input, work, err)
		}
	}
}

func TestPublicationForwardWorkComposesEveryExistingBeforeRead(t *testing.T) {
	testPublicationForwardWork(t, true)
}

func TestPublicationForwardWorkComposesAbsentBeforeConfirmation(t *testing.T) {
	testPublicationForwardWork(t, false)
}

func testPublicationForwardWork(t *testing.T, beforeExists bool) {
	t.Helper()
	root := mustCanonicalStateDir(t, t.TempDir())
	path := filepath.Join(root, "state.json")
	before := AbsentPublicationImage()
	if beforeExists {
		before = publicationImageForTest(t, []byte("Before"))
		writeFixture(t, path, "Before", 0o600)
	}
	after := publicationImageForTest(t, []byte("After"))
	intent, err := NewPublicationIntent([]byte("selection"), []PublicationTarget{
		publicationTargetForTest(t, path, before, after),
	})
	if err != nil {
		t.Fatal(err)
	}
	body, header, err := encodePublicationEvidence(intent)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := bindPublicationPlan(t.Context(), filepath.Join(root, "metadata"), intent,
		int64(len(header)), int64(len(body)), &fenceObservationBudget{})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.rootWitness.Close()
	tree, err := storagecommit.MaximumFlatRootedTreePublicationWork(plan.destination, plan.limits)
	if err != nil {
		t.Fatal(err)
	}
	r, err := plan.destination.RootValidationWork()
	if err != nil {
		t.Fatal(err)
	}
	renamePaths, err := storagecommit.MaximumRootedEntryRenamePathWork(plan.destination)
	if err != nil {
		t.Fatal(err)
	}
	m, err := fileSetAbsolutePathWork(markerPath(plan.root), mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		t.Fatal(err)
	}
	d, err := fileSetAbsolutePathWork(path, mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		t.Fatal(err)
	}
	// Two complete identity-guarded views contribute 14m-4. Backup-image loads
	// contribute another 9m. Target capture/classification/recheck/publication
	// and restoration contribute 25d; terminal re-read and persistence add5d.
	// Acquisitions contribute 4r; retirement observes at most eight entries.
	wantPaths := tree.PathWork() + renamePaths + 4*r + 23*m - 4 + 30*d
	wantEntries := tree.EntryWork() + 28
	wantBytes := tree.ByteWork() +
		2*(maximumMarkerBytes+maximumTargetBytes) +
		3*max(before.size, after.size) + 7*before.size + 2*after.size
	if !beforeExists {
		absence, err := storagecommit.MaximumRootedAbsenceConfirmationPathWork(d)
		if err != nil {
			t.Fatal(err)
		}
		// No backups or Before rewrite. Restoration removes the created file;
		// terminal confirmation captures/acquires its root before confirming
		// absence, even though the exclusive present branch needs only3d.
		wantPaths = tree.PathWork() + renamePaths + 4*r + 14*m - 4 + 41*d + absence
		wantEntries -= 4
		wantBytes += 3 * after.size
	}
	failure, err := plan.retirementFailureWork()
	if err != nil {
		t.Fatal(err)
	}
	wantPaths += failure.pathComponents
	wantEntries += failure.entries
	wantBytes += failure.bytes
	actual := &fenceObservationBudget{}
	if err := plan.admitWork(actual, publicationForwardWork); err != nil {
		t.Fatal(err)
	}
	if actual.paths != wantPaths || actual.entries != wantEntries || actual.bytes != wantBytes {
		t.Fatalf("incomplete aggregate work: got %+v, want paths=%d entries=%d bytes=%d", actual, wantPaths, wantEntries, wantBytes)
	}
}
