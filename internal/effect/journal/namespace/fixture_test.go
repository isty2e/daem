package namespace

import (
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func imageBudget(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func journalTestFilesystem() mutationfs.Store { return storagecommit.Adapter{} }
