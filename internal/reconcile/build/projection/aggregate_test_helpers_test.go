package projection

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/reconcile"
)

func buildAggregateDecisionsForTest(input AggregateInput) ([]reconcile.AggregateDecision, error) {
	input.Codecs = aggregatecodec.Catalog()
	return BuildAggregateDecisions(input)
}

func mustReconciliationResult(
	t *testing.T,
	managedPaths []reconcile.ManagedPathDecision,
	aggregates []reconcile.AggregateDecision,
) reconcile.Result {
	t.Helper()
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context: reconcile.ContextInspect, ManagedPaths: managedPaths, Aggregates: aggregates,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
