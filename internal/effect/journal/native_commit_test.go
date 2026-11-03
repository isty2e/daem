package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func TestNativeDocumentCommitRequiresRealPublication(t *testing.T) {
	before := durable.EmptySnapshot()
	after, err := before.AdvanceNativeDocumentCommit()
	if err != nil {
		t.Fatal(err)
	}
	if err := requireNativeDocumentCommitWork(before, after, false); err == nil {
		t.Fatal("counter-only no-op acquired commit authority")
	}
	if err := requireNativeDocumentCommitWork(before, after, true); err != nil {
		t.Fatal(err)
	}
	if err := requireNativeDocumentCommitWork(after, before, true); err == nil {
		t.Fatal("commit decrement was admitted")
	}
	jump, err := after.AdvanceNativeDocumentCommit()
	if err != nil {
		t.Fatal(err)
	}
	if err := requireNativeDocumentCommitWork(before, jump, true); err == nil {
		t.Fatal("commit jump was admitted")
	}
}
