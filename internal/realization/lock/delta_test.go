package lock_test

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestBuildDeltaClassifiesAndOrdersGenericLockedSubjects(t *testing.T) {
	unchanged := skillSupply(t, "keep", "stable")
	changedBefore := skillSupply(t, "project", "before")
	changedAfter := skillSupply(t, "project", "after")
	removed := skillSupply(t, "cleanup", "removed")
	added := skillSupply(t, "add", "added")

	result := lock.BuildDelta(
		snapshottest.File(t, unchanged, removed, changedBefore),
		snapshottest.File(t, added, changedAfter, unchanged),
	)

	entries := result.Entries()
	want := []struct {
		status lock.DeltaStatus
		key    string
	}{
		{status: lock.DeltaStatusAdded, key: added.SubjectID().String()},
		{status: lock.DeltaStatusRemoved, key: removed.SubjectID().String()},
		{status: lock.DeltaStatusUnchanged, key: unchanged.SubjectID().String()},
		{status: lock.DeltaStatusChanged, key: changedBefore.SubjectID().String()},
	}
	if len(entries) != len(want) {
		t.Fatalf("len(Entries()) = %d, want %d", len(entries), len(want))
	}
	for index, expected := range want {
		entry := entries[index]
		if entry.Status != expected.status || entry.Key.String() != expected.key {
			t.Fatalf("Entries()[%d] = (%s, %s), want (%s, %s)",
				index, entry.Status, entry.Key, expected.status, expected.key)
		}
	}

	if !entries[0].Before.SubjectID().IsZero() || entries[0].After.SubjectID() != added.SubjectID() {
		t.Fatalf("added entry did not preserve only the after contract")
	}
	if entries[1].Before.SubjectID() != removed.SubjectID() || !entries[1].After.SubjectID().IsZero() {
		t.Fatalf("removed entry did not preserve only the before contract")
	}
	if !entries[2].Before.Equal(unchanged) || !entries[2].After.Equal(unchanged) {
		t.Fatalf("unchanged entry did not preserve both contracts")
	}
	if !entries[3].Before.Equal(changedBefore) || !entries[3].After.Equal(changedAfter) {
		t.Fatalf("changed entry did not preserve both contracts")
	}

	counts := result.Counts()
	if counts != (lock.DeltaCounts{Added: 1, Changed: 1, Removed: 1, Unchanged: 1}) {
		t.Fatalf("Counts() = %+v, want one of every status", counts)
	}
	if !result.HasChanges() {
		t.Fatalf("HasChanges() = false, want true")
	}
}

func TestDeltaAccessorsReturnIndependentSlices(t *testing.T) {
	contract := skillSupply(t, "stable", "content")
	result := lock.BuildDelta(snapshottest.File(t, contract), snapshottest.File(t, contract))

	entries := result.Entries()
	entries[0].Status = lock.DeltaStatusRemoved
	if got := result.Entries()[0].Status; got != lock.DeltaStatusUnchanged {
		t.Fatalf("Entries() mutation changed delta status to %q", got)
	}

	unchanged := result.EntriesWithStatus(lock.DeltaStatusUnchanged)
	unchanged[0].Status = lock.DeltaStatusAdded
	if got := result.EntriesWithStatus(lock.DeltaStatusUnchanged)[0].Status; got != lock.DeltaStatusUnchanged {
		t.Fatalf("EntriesWithStatus() mutation changed delta status to %q", got)
	}
	if result.HasChanges() {
		t.Fatalf("HasChanges() = true for an unchanged delta")
	}
}

func skillSupply(t *testing.T, name string, content string) lock.LockedSubjectContract {
	t.Helper()
	return snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindSkill,
		Name:         name,
		SourceID:     artifact.SourceID("local:" + name + "?mode=vendor"),
		ArtifactKind: artifact.ArtifactKindDirectory,
		ContentHash:  artifact.HashFileContent([]byte(content)),
	})
}
