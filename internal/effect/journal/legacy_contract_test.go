package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func mustPersistedAggregateContract(t testing.TB, contract aggregate.ProjectionContract) *recoveryAggregateContract {
	t.Helper()
	persisted, err := persistedAggregateContract(contract)
	if err != nil {
		t.Fatal(err)
	}
	return persisted
}
