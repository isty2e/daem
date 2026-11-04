package ownership

import (
	"path/filepath"
	"testing"
)

func TestProtectedDocumentRequiresItsExactRecordAndCurrentCompleteCohort(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "settings.json")
	alpha := testDocumentBinding(t, root, "alpha", ".pi/settings.json", physical)
	beta := testDocumentBinding(t, root, "beta", "@pi-agent/settings.json", physical)
	row := testPendingProtection(t, alpha, beta)
	registry, err := NewRegistryWithProtections(nil, []DocumentBinding{beta, alpha}, []PendingProtection{row})
	if err != nil {
		t.Fatal(err)
	}
	document := row.Documents()[0]
	if err := registry.RequireProtectedDocument(row, document); err != nil {
		t.Fatal(err)
	}
	changed, err := NewRegistryWithProtections(nil, []DocumentBinding{alpha}, []PendingProtection{row})
	if err != nil {
		t.Fatal(err)
	}
	if err := changed.RequireProtectedDocument(row, document); err == nil {
		t.Fatal("missing co-owner was accepted")
	}
	absent, err := NewRegistry(nil, alpha, beta)
	if err != nil {
		t.Fatal(err)
	}
	if err := absent.RequireProtectedDocument(row, document); err == nil {
		t.Fatal("absent protection authorized a document")
	}
	bound, err := row.Bind(testProtectionExecution(t, row.Invocation()))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RequireProtectedDocument(bound, document); err == nil {
		t.Fatal("different execution binding reused an armed observation")
	}
	other := testDocumentBinding(t, root, "alpha", "other.json", filepath.Join(root, "other.json"))
	foreign := testPendingProtection(t, other).Documents()[0]
	if err := registry.RequireProtectedDocument(row, foreign); err == nil {
		t.Fatal("a document outside the frozen set was admitted")
	}
}
