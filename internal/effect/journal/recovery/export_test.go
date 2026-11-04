package recovery

import "testing"

func CaptureBudgetDemandsForTest(t *testing.T) RemovalDemandSet {
	t.Helper()
	set, err := NewRemovalDemandSet([]RemovalDemand{testRemovalIntent(t).Demand()})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func InvalidCaptureBudgetDemandsForTest(set RemovalDemandSet) map[string]RemovalDemandSet {
	return map[string]RemovalDemandSet{
		"invalid":    {demands: []RemovalDemand{{}}},
		"duplicates": {demands: []RemovalDemand{set.demands[0], set.demands[0]}},
		"over-limit": {demands: make([]RemovalDemand, MaximumRemovalIntents+1)},
	}
}
