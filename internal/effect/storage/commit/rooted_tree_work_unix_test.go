//go:build darwin || linux

package commit

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestFlatRootedTreePublicationWorkIncludesFailureAndNamespaceWork(t *testing.T) {
	root := captureRootForCommitTest(t, t.TempDir())
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination("missing/metadata/transaction")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(9, 0, 17)
	if err != nil {
		t.Fatal(err)
	}
	work, err := MaximumFlatRootedTreePublicationWork(destination, limits)
	if err != nil {
		t.Fatal(err)
	}
	r, err := destination.RootValidationWork()
	if err != nil {
		t.Fatal(err)
	}
	w, err := destination.ParentChainValidationWork()
	if err != nil {
		t.Fatal(err)
	}
	minimumEntries := 9 + 16*9 + 9*9 + 2
	if work.EntryWork() < minimumEntries || work.ByteWork() != 156 ||
		work.PathWork() != 73*(w+2*r)+116*r+r+2 {
		t.Fatalf("incomplete publication work: %+v", work)
	}
	if _, err := os.Lstat(filepath.Join(authority.PhysicalRoot(), "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pure work projection touched destination: %v", err)
	}

	emptyLimits, err := mutationfs.NewTreeTraversalLimits(0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := MaximumFlatRootedTreePublicationWork(destination, emptyLimits)
	if err != nil || empty.EntryWork() != 2 || empty.ByteWork() != 3 || empty.PathWork() <= 0 ||
		empty.PathWork() >= work.PathWork() {
		t.Fatalf("empty tree lost lifecycle work: %+v, %v", empty, err)
	}
}

func TestFlatRootedTreePublicationWorkRejectsInvalidAndOverflowingInputs(t *testing.T) {
	root := captureRootForCommitTest(t, t.TempDir())
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination("transaction")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	maximumInt := int(^uint(0) >> 1)
	for _, test := range []struct {
		name    string
		entries int
		depth   int
		bytes   int64
	}{
		{name: "nested", entries: 1, depth: 1},
		{name: "entry overflow", entries: maximumInt},
		{name: "byte overflow", bytes: math.MaxInt64},
		{name: "path overflow", entries: maximumInt / 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits, err := mutationfs.NewTreeTraversalLimits(test.entries, test.depth, test.bytes)
			if err != nil {
				t.Fatal(err)
			}
			work, err := MaximumFlatRootedTreePublicationWork(destination, limits)
			if err == nil || work != (FlatRootedTreePublicationWork{}) {
				t.Fatalf("invalid work accepted: %+v, %v", work, err)
			}
		})
	}
	limits, err := mutationfs.NewTreeTraversalLimits(0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MaximumFlatRootedTreePublicationWork(rootedpath.Destination{}, limits); err == nil {
		t.Fatal("uninitialized destination accepted")
	}
	if _, err := MaximumFlatRootedTreePublicationWork(destination, mutationfs.TreeTraversalLimits{}); err == nil {
		t.Fatal("uninitialized limits accepted")
	}
}
