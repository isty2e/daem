package durable_test

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func TestNativeDocumentCommitDistinguishesStateNeutralPublication(t *testing.T) {
	before := durable.EmptySnapshot()
	after, err := before.AdvanceNativeDocumentCommit()
	if err != nil || after.Equal(before) || before.NativeDocumentCommit() != 0 || after.NativeDocumentCommit() != 1 {
		t.Fatal("native document publication did not create distinct immutable state", err)
	}
	retained, err := after.WithManagedAggregates(nil)
	if err != nil || !retained.Equal(after) {
		t.Fatal("another snapshot transformation discarded the native commit", err)
	}
	maximum, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: ^uint64(0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := maximum.AdvanceNativeDocumentCommit(); err == nil {
		t.Fatal("native commit overflow silently wrapped")
	}
}
