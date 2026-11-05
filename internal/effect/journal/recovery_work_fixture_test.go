package journal

import (
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func mustManifestAuthoritySession(t *testing.T, root string) *journalnamespace.ManifestAuthority {
	t.Helper()
	session, err := journalnamespace.CaptureManifestAuthority(root, nil, nil)
	if err != nil {
		t.Fatalf("capture manifest authority: %v", err)
	}
	return session
}

func newRecoveryPathBudget(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatalf("new recovery path budget: %v", err)
	}
	return budget
}

func recoveryBackupBudgetForTest(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatalf("construct recovery backup work budget: %v", err)
	}
	return budget
}
