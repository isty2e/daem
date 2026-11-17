package qualification_test

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func hasRootedPathFailureKind(err error, kind rootedpath.FailureKind) bool {
	var failure *rootedpath.Failure
	return errors.As(err, &failure) && failure.Kind() == kind
}

func newRecoveryPathBudget(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatalf("new recovery path budget: %v", err)
	}
	return budget
}
