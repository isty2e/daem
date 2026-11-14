package controlentry

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func TestJournalEntriesOwnPairButNotSuppliedRoot(t *testing.T) {
	fixture := newSelectedControlFixture(t)
	directory := fixture.journalDirectory(t)
	var entries JournalEntries
	t.Cleanup(func() { _ = entries.CloseInto(nil) })
	if err := entries.Bind(fixture.root, fixture.path, directory, fixture.phase); err != nil {
		t.Fatal(err)
	}
	boundDirectory, boundRecord := entries.Directory(), entries.Record()
	requireUsableControlEntry(t, boundDirectory)
	requireUsableControlEntry(t, boundRecord)

	if err := entries.Bind(nil, fixture.path, directory, nil); err == nil || err.Error() != "recovery journal authority is already bound" {
		t.Fatalf("rebind did not precede unavailable-phase admission: %v", err)
	}
	if entries.Directory() != boundDirectory || entries.Record() != boundRecord {
		t.Fatal("refused rebind replaced the existing pair")
	}
	prior := errors.New("earlier namespace failure")
	if got := entries.CloseInto(prior); got != prior {
		t.Fatalf("successful release changed the prior error fold: %v", got)
	}
	if entries.Directory() != nil || entries.Record() != nil {
		t.Fatal("closed journal retained its pair")
	}
	for _, entry := range []*rootedpath.EntryAuthority{boundDirectory, boundRecord} {
		if capability, err := entry.Acquire(); err == nil {
			_ = capability.Close()
			t.Fatal("closed journal entry retained authority")
		}
	}
	if err := fixture.root.ValidateSelectionBounded(fixture.path, physicalwork.MaximumPhysicalPathDepth, fixture.phase); err != nil {
		t.Fatalf("pair cleanup closed the caller's root: %v", err)
	}
	if got := entries.CloseInto(prior); got != prior {
		t.Fatal("repeated cleanup changed the prior error")
	}
}

func TestJournalEntriesRollBackFailedRecordBinding(t *testing.T) {
	fixture := newSelectedControlFixture(t)
	directory := fixture.journalDirectory(t)
	acquired, err := bindSelectedEntry(fixture.root, fixture.path, directory, fixture.phase)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = acquired.Close() })
	refusal := errors.New("record binding failed")
	var entries JournalEntries
	t.Cleanup(func() { _ = entries.CloseInto(nil) })
	var attempted []string
	err = entries.bindEntries(directory, func(path string) (*rootedpath.EntryAuthority, error) {
		attempted = append(attempted, path)
		if len(attempted) == 1 {
			return acquired, nil
		}
		return nil, refusal
	})
	if !errors.Is(err, refusal) {
		t.Fatalf("record failure lost its cause: %v", err)
	}
	if len(attempted) != 2 || attempted[0] != directory || attempted[1] != filepath.Join(directory, "journal.json") {
		t.Fatalf("journal binding paths = %v", attempted)
	}
	if entries.Directory() != nil || entries.Record() != nil {
		t.Fatal("failed binding published a partial pair")
	}
	if capability, err := acquired.Acquire(); err == nil {
		_ = capability.Close()
		t.Fatal("failed record binding leaked its first entry")
	}
	if err := fixture.root.ValidateSelectionBounded(fixture.path, physicalwork.MaximumPhysicalPathDepth, fixture.phase); err != nil {
		t.Fatalf("failed binding closed the caller's root: %v", err)
	}
	if err := entries.Bind(fixture.root, fixture.path, directory, fixture.phase); err != nil {
		t.Fatalf("failed binding retained a poisoned pair: %v", err)
	}
	if err := entries.CloseInto(nil); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyJournalEntriesPreservePriorError(t *testing.T) {
	prior := errors.New("earlier failure")
	var empty JournalEntries
	for _, entries := range []*JournalEntries{nil, &empty} {
		if entries.Directory() != nil || entries.Record() != nil {
			t.Fatal("empty journal returned a control entry")
		}
		if got := entries.CloseInto(prior); got != prior {
			t.Fatal("empty cleanup lost or rewrapped its prior error")
		}
		if got := entries.CloseInto(nil); got != nil {
			t.Fatalf("empty cleanup introduced an error: %v", got)
		}
	}
}
