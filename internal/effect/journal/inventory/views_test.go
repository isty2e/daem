package inventory

import (
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestInventoryProjectionsCannotOverwriteObservedEvidence(t *testing.T) {
	identity, err := retirement.NewIdentity("observed-journal", journalrecord.Fingerprint([]byte("record")))
	if err != nil {
		t.Fatal(err)
	}
	physical := directoryIdentity("observed-root")
	authority, err := NewActiveAuthority(physical)
	if err != nil {
		t.Fatal(err)
	}
	control, err := mutationfs.NewDirectorySnapshot(physical, 0o700, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantControl := control
	observed := RootInventory{
		active: &ActiveJournal{
			journalDirectoryEvidence: journalDirectoryEvidence{identity: identity, operationDir: "observed-journal"},
			physicalAuthority:        authority,
		},
		control: &control,
	}

	*observed.Active() = ActiveJournal{}
	*observed.ControlSnapshot() = mutationfs.DirectorySnapshot{}
	active := observed.Active()
	if active.Identity() != identity || active.OperationDir() != "observed-journal" || !active.PhysicalAuthority().Equal(authority) {
		t.Error("active projection overwrote inventory evidence")
	}
	if !observed.ControlSnapshot().Equal(wantControl) {
		t.Error("control projection overwrote inventory evidence")
	}
	if (RootInventory{}).Active() != nil || (RootInventory{}).ControlSnapshot() != nil {
		t.Error("empty inventory supplied observed evidence")
	}
}

func TestActiveJournalSourceRequiresEveryObservedIdentityComponent(t *testing.T) {
	identity := func(operation, fingerprint string) retirement.Identity {
		t.Helper()
		value, err := retirement.NewIdentity(operation, fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	authority := func(value string) ActiveJournalAuthority {
		t.Helper()
		result, err := NewActiveAuthority(directoryIdentity(value))
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	fingerprint := journalrecord.Fingerprint([]byte("record"))
	source := ActiveJournal{
		journalDirectoryEvidence: journalDirectoryEvidence{identity: identity("observed-journal", fingerprint), operationDir: "selected/observed-journal"},
		physicalAuthority:        authority("observed-root"),
	}
	if !source.SameSource(source) || (ActiveJournal{}).SameSource(ActiveJournal{}) {
		t.Fatal("source identity did not distinguish observed from zero evidence")
	}
	for _, test := range []struct {
		name   string
		change func(*ActiveJournal)
	}{
		{"operation", func(other *ActiveJournal) { other.identity = identity("another-journal", fingerprint) }},
		{"directory", func(other *ActiveJournal) { other.operationDir = "another/observed-journal" }},
		{"bytes", func(other *ActiveJournal) {
			other.identity = identity("observed-journal", journalrecord.Fingerprint([]byte("other record")))
		}},
		{"physical", func(other *ActiveJournal) { other.physicalAuthority = authority("another-incarnation") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := source
			test.change(&other)
			if source.SameSource(other) || other.SameSource(source) {
				t.Fatal("different source was accepted")
			}
		})
	}
}

type directoryIdentity string

func (identity directoryIdentity) Kind() mutationfs.EntryKind { return mutationfs.EntryKindDirectory }

func (identity directoryIdentity) Equal(other mutationfs.EntryIdentity) bool {
	return identity == other
}
