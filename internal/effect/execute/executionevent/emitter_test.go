package executionevent

import (
	"errors"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestEmitterRetainsEventFieldsAndSeparateActionSnapshots(t *testing.T) {
	cause := errors.New("selected failure")
	facts := ActionFacts{Index: 3, Target: target.TargetCodex, Scope: target.ScopeProject, Destination: outputtest.Parse(t, "managed")}
	var observed []Event
	emitter := NewEmitter(func(event Event) {
		observed = append(observed, event)
	}, 7)
	emitter.Emit(ActionStarted, StageAction, facts.Snapshot(), nil)
	emitter.Emit(ActionFailed, StageAction, facts.Snapshot(), cause)
	emitter.Emit(JournalCaptureFailed, StageJournalCapture, nil, cause)
	if len(observed) != 3 {
		t.Fatalf("emitted events=%d, want three", len(observed))
	}
	for _, event := range observed {
		if event.TotalActions != 7 {
			t.Fatalf("event action count=%d, want seven", event.TotalActions)
		}
	}
	if string(observed[0].Kind) != "action_started" || string(observed[0].Stage) != "action" || observed[0].Err != nil ||
		observed[0].Action == nil || !reflect.DeepEqual(*observed[0].Action, facts) {
		t.Fatalf("started event lost source facts: %#v", observed[0])
	}
	if string(observed[1].Kind) != "action_failed" || observed[1].Stage != StageAction || !errors.Is(observed[1].Err, cause) ||
		observed[1].Action == nil || observed[1].Action.Index != 3 {
		t.Fatalf("failed event lost source facts: %#v", observed[1])
	}
	if string(observed[2].Kind) != "journal_capture_failed" || string(observed[2].Stage) != "journal_capture" ||
		observed[2].Action != nil || !errors.Is(observed[2].Err, cause) {
		t.Fatalf("capture event lost source facts: %#v", observed[2])
	}
	observed[0].Action.Index = 99
	if facts.Index != 3 || observed[1].Action.Index != 3 {
		t.Fatal("one action snapshot changed another scalar projection")
	}
}

func TestNilSinkAndZeroEmitterAreNoOps(t *testing.T) {
	var sink Sink
	sink.Emit(Event{Kind: RelationOrderDone})
	var emitter Emitter
	emitter.Emit(ActionDone, StageAction, nil, nil)
	NewEmitter(nil, 4).Emit(JournalCleaned, StageJournalCleanup, nil, nil)
}

func TestSinkRetainsDirectRelationOrderFacts(t *testing.T) {
	facts := &RelationOrderFacts{Target: target.TargetCodex, Scope: target.ScopeProject, Changed: true}
	var observed Event
	Sink(func(event Event) { observed = event }).Emit(Event{
		Kind: RelationOrderDone, Stage: StageRelationOrder, RelationOrder: facts,
	})
	if observed.Kind != RelationOrderDone || observed.Stage != StageRelationOrder || observed.RelationOrder != facts || observed.Action != nil {
		t.Fatalf("direct relation-order event changed: %#v", observed)
	}
}
