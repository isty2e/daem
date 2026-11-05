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

type directoryIdentity string

func (identity directoryIdentity) Kind() mutationfs.EntryKind { return mutationfs.EntryKindDirectory }

func (identity directoryIdentity) Equal(other mutationfs.EntryIdentity) bool {
	return identity == other
}
