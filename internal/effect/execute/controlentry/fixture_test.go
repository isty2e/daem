package controlentry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

type selectedControlFixture struct {
	path       string
	root       *rootedpath.CapturedRoot
	allocation *physicalwork.Budget
	phase      *physicalwork.TraversalPhase
}

func newSelectedControlFixture(t *testing.T) selectedControlFixture {
	t.Helper()
	path := t.TempDir()
	allocation, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	phase, err := physicalwork.NewTraversalPhase(allocation)
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRootBounded(path, physicalwork.MaximumPhysicalPathDepth, phase)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return selectedControlFixture{path: path, root: root, allocation: allocation, phase: phase}
}

func (fixture selectedControlFixture) journalDirectory(t *testing.T) string {
	t.Helper()
	path := filepath.Join(fixture.path, "operation")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireUsableControlEntry(t *testing.T, entry *rootedpath.EntryAuthority) {
	t.Helper()
	if entry == nil {
		t.Fatal("missing control entry")
	}
	capability, err := entry.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
}
