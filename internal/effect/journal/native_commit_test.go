package journal

import (
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func TestNativeDocumentCommitRequiresRealPublication(t *testing.T) {
	before := durable.EmptySnapshot()
	after, err := before.AdvanceNativeDocumentCommit()
	if err != nil {
		t.Fatal(err)
	}
	if err := journalrecord.RequireDocumentCommitWork(before, after, false); err == nil {
		t.Fatal("counter-only no-op acquired commit authority")
	}
	if err := journalrecord.RequireDocumentCommitWork(before, after, true); err != nil {
		t.Fatal(err)
	}
	if err := journalrecord.RequireDocumentCommitWork(after, before, true); err == nil {
		t.Fatal("commit decrement was admitted")
	}
	jump, err := after.AdvanceNativeDocumentCommit()
	if err != nil {
		t.Fatal(err)
	}
	if err := journalrecord.RequireDocumentCommitWork(before, jump, true); err == nil {
		t.Fatal("commit jump was admitted")
	}
}
