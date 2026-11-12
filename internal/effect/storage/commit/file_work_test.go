package commit

import (
	"math"
	"testing"
)

func TestFileCommitWorkIncludesWriteVerificationAndNamespaceCleanup(t *testing.T) {
	work, err := MaximumFileCommitWork(5, 17)
	if err != nil || work.PathWork() != 40 || work.EntryWork() != 1 || work.ByteWork() != 34 {
		t.Fatal("file publication lost storage work", work, err)
	}
	empty, err := MaximumFileCommitWork(1, 0)
	if err != nil || empty.PathWork() != 8 || empty.EntryWork() != 1 || empty.ByteWork() != 0 {
		t.Fatal("empty file lost namespace work", empty, err)
	}
}

func TestFileCommitWorkRejectsInvalidAndOverflowingDimensions(t *testing.T) {
	for _, test := range []struct {
		depth int
		bytes int64
	}{
		{depth: 0},
		{depth: -1},
		{depth: 1, bytes: -1},
		{depth: int(^uint(0)>>1)/8 + 1},
		{depth: 1, bytes: math.MaxInt64},
	} {
		work, err := MaximumFileCommitWork(test.depth, test.bytes)
		if err == nil || work != (FileWork{}) {
			t.Fatal("invalid dimensions acquired file work", test, work, err)
		}
	}
}

func TestFileReadWorkIncludesItsActualPrimitiveTraversals(t *testing.T) {
	snapshot, err := MaximumFileSnapshotWork(5, 17)
	if err != nil || snapshot.PathWork() != 10 || snapshot.EntryWork() != 1 || snapshot.ByteWork() != 17 {
		t.Fatal("snapshot lost parent opening, chain seal or payload work", snapshot, err)
	}
	identity, err := MaximumFileIdentityWork(5)
	if err != nil || identity.PathWork() != 5 || identity.EntryWork() != 1 || identity.ByteWork() != 0 {
		t.Fatal("identity observation acquired the wrong work", identity, err)
	}
	for _, input := range []struct {
		depth int
		bytes int64
	}{{depth: 0}, {depth: -1}, {depth: 1, bytes: -1}, {depth: int(^uint(0)>>1)/2 + 1}} {
		if work, err := MaximumFileSnapshotWork(input.depth, input.bytes); err == nil || work != (FileWork{}) {
			t.Fatal("invalid snapshot work accepted", input, work, err)
		}
	}
	if _, err := MaximumFileIdentityWork(0); err == nil {
		t.Fatal("unbounded identity observation accepted")
	}
}

func TestFilePersistenceWorkIncludesOpeningAndBothChainSeals(t *testing.T) {
	work, err := MaximumFilePersistenceWork(5)
	if err != nil || work.PathWork() != 15 || work.EntryWork() != 1 || work.ByteWork() != 0 {
		t.Fatal("persistence work lost a chain seal or read payload", work, err)
	}
	for _, depth := range []int{0, -1, int(^uint(0)>>1)/3 + 1} {
		if work, err := MaximumFilePersistenceWork(depth); err == nil || work != (FileWork{}) {
			t.Fatal("invalid persistence work accepted", depth, work, err)
		}
	}
}
