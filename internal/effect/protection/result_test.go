package protection

import (
	"errors"
	"testing"
)

func TestResultOwnsSnapshotsAndRefusalRetainsIndependentFacts(t *testing.T) {
	completed := Record{settlement: SettlementCleared, targets: []Target{{state: TargetSecure, expanded: true}}}
	blocked := Record{settlement: SettlementPending, targets: []Target{{state: TargetBlocked}}}
	records := []Record{completed, blocked}
	result := NewResult(records)
	records[0].targets[0].state = TargetBlocked
	snapshot := result.Records()
	if result.Complete() || result.Phase() != ResultPending || snapshot[0].targets[0].state != TargetSecure || !snapshot[0].targets[0].expanded {
		t.Fatal("partial result lost its independent completed target")
	}
	snapshot[0].targets[0].state = TargetAbsent
	if result.Records()[0].targets[0].state != TargetSecure {
		t.Fatal("result exposed its retained target state")
	}

	cause := errors.New("fresh authority could not be established")
	refused := snapshot[0].Unobserved().Refused(cause)
	unknown := NewResult([]Record{refused, completed})
	if unknown.Phase() != ResultUnknown || !errors.Is(unknown.Failure(), cause) || !refused.Targets()[0].Expanded() {
		t.Fatal("refusal lost uncertainty, cause or observed expansion")
	}
	if snapshot[0].targets[0].state != TargetAbsent || result.Records()[0].targets[0].state != TargetSecure {
		t.Fatal("refusal changed an earlier snapshot")
	}
	if (Result{}).Complete() || (Result{}).Phase() != ResultPending {
		t.Fatal("empty evidence became completed protection")
	}
}
