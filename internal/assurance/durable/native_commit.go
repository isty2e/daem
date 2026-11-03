package durable

import "fmt"

// NativeDocumentCommit distinguishes publication of a state-neutral native
// document edit. It is not selected-value or mutation authority.
func (snapshot Snapshot) NativeDocumentCommit() uint64 { return snapshot.nativeDocumentCommit }

func (snapshot Snapshot) AdvanceNativeDocumentCommit() (Snapshot, error) {
	if snapshot.nativeDocumentCommit == ^uint64(0) {
		return Snapshot{}, fmt.Errorf("native document commit counter is exhausted")
	}
	input := snapshot.input()
	input.NativeDocumentCommit++
	return NewSnapshot(input)
}

// ValidateNativeDocumentCommitTransition admits an unchanged commit or exactly
// one advance with no other durable fact changes.
func (snapshot Snapshot) ValidateNativeDocumentCommitTransition(after Snapshot) error {
	if snapshot.nativeDocumentCommit == after.nativeDocumentCommit {
		return nil
	}
	expected, err := snapshot.AdvanceNativeDocumentCommit()
	if err != nil {
		return err
	}
	if !expected.Equal(after) {
		return fmt.Errorf("native document commit must advance exactly once without changing other durable facts")
	}
	return nil
}
