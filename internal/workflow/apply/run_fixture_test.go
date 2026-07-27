package apply

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute"
	targetpkg "github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func assertHookAggregateState(t *testing.T, snapshot durable.Snapshot, expectedSubject topology.SubjectID) {
	t.Helper()
	for _, stateResource := range snapshot.ManagedAggregates() {
		if stateResource.Subject() != expectedSubject {
			continue
		}
		contribution := stateResource.Contribution()
		if contribution.Target() != targetpkg.TargetCodex || contribution.Scope() != targetpkg.ScopeProject ||
			contribution.AggregateRoot().String() != ".codex/hooks.json" || contribution.ContentPath() != "/hooks" ||
			!strings.Contains(contribution.CanonicalContribution(), "python3 hooks/protect.py") {
			t.Fatalf("managed Hook aggregate state = %#v", stateResource)
		}
		return
	}
	t.Fatalf("managed Hook aggregate subject %q not found in %#v", expectedSubject, snapshot.ManagedAggregates())
}

func assertWorkflowApplyEventKinds(t *testing.T, events []execute.Event, wants ...execute.EventKind) {
	t.Helper()

	var got []string
	for _, event := range events {
		got = append(got, string(event.Kind))
	}
	joined := strings.Join(got, ",")
	for _, want := range wants {
		if !strings.Contains(joined, string(want)) {
			t.Fatalf("event kinds = %#v, want %q", got, want)
		}
	}
}
