package reconcile

import "testing"

func mustReconciliationResult(
	t testing.TB,
	managedPaths []ManagedPathDecision,
	aggregates []AggregateDecision,
) Result {
	t.Helper()
	result, err := NewResult(ResultInput{
		Context:      ContextInspect,
		ManagedPaths: managedPaths,
		Aggregates:   aggregates,
	})
	if err != nil {
		t.Fatalf("NewResult returned error: %v", err)
	}
	return result
}
