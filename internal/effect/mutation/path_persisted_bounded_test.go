package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type pathAuthorityTestBudget struct {
	limit  int
	visits int
}

func (budget *pathAuthorityTestBudget) AdmitPathComponents(count int) error {
	if count < 0 || budget.visits+count > budget.limit {
		return errors.New("injected path-authority budget exhausted")
	}
	budget.visits += count
	return nil
}

func TestDirectoryEntryAuthorityBoundedRequiresCapacityBeforeObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write authority fixture: %v", err)
	}

	exhausted := &pathAuthorityTestBudget{}
	if _, err := CanonicalDirectoryEntryKeyBounded(path, 256, exhausted); err == nil {
		t.Fatal("bounded directory-entry key accepted an exhausted budget")
	}
	if exhausted.visits != 0 {
		t.Fatalf("failed key observation charged %d visits, want atomic rejection", exhausted.visits)
	}
	if _, err := ObserveDirectoryEntryAuthorityBounded(path, 256, exhausted); err == nil {
		t.Fatal("bounded directory-entry authority accepted an exhausted budget")
	}
	if exhausted.visits != 0 {
		t.Fatalf("failed authority observation charged %d visits, want atomic rejection", exhausted.visits)
	}

	unboundedKey, err := CanonicalDirectoryEntryKey(path)
	if err != nil {
		t.Fatalf("unbounded directory-entry key: %v", err)
	}
	available := &pathAuthorityTestBudget{limit: 100_000}
	if key, err := CanonicalDirectoryEntryKeyBounded(path, 256, available); err != nil {
		t.Fatalf("bounded directory-entry key: %v", err)
	} else if key != unboundedKey {
		t.Fatalf("bounded directory-entry key = %q, want canonical key %q", key, unboundedKey)
	}
	if _, err := ObserveDirectoryEntryAuthorityBounded(path, 256, available); err != nil {
		t.Fatalf("bounded directory-entry authority: %v", err)
	}
	if available.visits == 0 {
		t.Fatal("bounded directory-entry authority performed no charged path work")
	}

	unboundedPersisted, err := ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatalf("unbounded persisted authority: %v", err)
	}
	persistedBudget := &pathAuthorityTestBudget{limit: 100_000}
	boundedPersisted, err := ObservePersistedDirectoryEntryAuthorityBounded(path, 256, persistedBudget)
	if err != nil {
		t.Fatalf("bounded persisted authority: %v", err)
	}
	if !boundedPersisted.Exact().Equal(unboundedPersisted.Exact()) {
		t.Fatal("bounded persisted authority changed canonical path semantics")
	}
	if persistedBudget.visits == 0 {
		t.Fatal("bounded persisted authority performed no charged path work")
	}
}
