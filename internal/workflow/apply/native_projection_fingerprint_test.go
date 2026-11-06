package apply

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestAggregateApprovalFactsDistinguishMissingAndBlockingAncestors(t *testing.T) {
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, outputtest.Parse(t, "@pi-agent/settings.json"), outputtest.KeyFootprint(t, "a", "b", "c"), "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := aggregate.NewProjectionState(contract, true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	states := []aggregate.ProjectionState{missing}
	for _, segments := range [][]string{{"a"}, {"a", "b"}} {
		ancestor, err := output.NewKeyPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		blocked, err := aggregate.NewBlockedProjectionState(contract, ancestor)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, blocked)
	}
	var encodings [][]byte
	for _, state := range states {
		encoded, err := json.Marshal(aggregateProjectionStateFingerprint(state))
		if err != nil {
			t.Fatal(err)
		}
		for _, earlier := range encodings {
			if bytes.Equal(earlier, encoded) {
				t.Fatal("approval projection collapsed different native read facts")
			}
		}
		encodings = append(encodings, encoded)
	}
}
