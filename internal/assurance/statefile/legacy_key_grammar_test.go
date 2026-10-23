package statefile

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func TestLegacyStateKeyGrammarIsNotReinterpretedAsRFC6901(t *testing.T) {
	state := testV9AggregateState(t)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: []durable.ManagedAggregateState{state}})
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{8, 9} {
		legacy := legacySnapshotForTest(t, snapshot, version)
		original, err := json.Marshal(legacy)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(original)
		if err != nil || !snapshot.Equal(decoded) {
			t.Fatalf("v%d changed an admitted legacy scope: %v", version, err)
		}
		for _, spelling := range []string{"/hooks~1suffix", "/hooks~0suffix", "/hooks%2Fsuffix"} {
			candidate := legacy
			candidate.ManagedAggregateBaselines = append([]legacyManagedAggregateDTO(nil), legacy.ManagedAggregateBaselines...)
			candidate.ManagedAggregateBaselines[0].ContentPath = spelling
			content, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(content); err == nil {
				t.Fatalf("v%d admitted a previously forbidden native/URI spelling %q", version, spelling)
			}
		}
	}
}
