package execute

import (
	"context"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute/executionevent"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/test/testkit/stateimage"
)

func TestOrdinaryApplySeparatesPhysicalStateAndJournalEncoding(t *testing.T) {
	fixture := newApplyEventFixture(t)
	input := fixture.input([]applyEventAction{fixture.createAction("create", "CREATE.md", "created\n")})
	input.StatefileCodec = stateimage.NewCodec(t, input.Paths.StatefilePath)
	next, err := deriveApplyStateTransition(input)
	if err != nil {
		t.Fatal(err)
	}
	observed := false
	result, err := ApplyWithOptions(t.Context(), input, ApplyOptions{Events: func(event executionevent.Event) {
		if event.Kind == executionevent.JournalCaptured {
			observed = stateimage.CheckPublishedJournal(t, input.Paths.RecoveryDir, input.StateCodec, input.CurrentState, next.nextState)
		}
	}})
	if err != nil || !observed {
		t.Fatal("ordinary apply did not retain its frozen journal encoding", err)
	}
	content, err := os.ReadFile(input.Paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	image, err := statefile.UnmarshalImage(content)
	if err != nil || !image.Snapshot().Equal(result.State) {
		t.Fatal("physical publication did not use its selected image encoder", err)
	}
	if _, err := input.StateCodec.Decode(content); err == nil {
		t.Fatal("image fixture does not distinguish physical bytes from journal bytes")
	}
	journalBytes, err := input.StateCodec.Encode(result.State)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := input.StatefileCodec.DecodeState(journalBytes); err == nil {
		t.Fatal("image fixture silently accepts journal bytes as a physical State")
	}
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestOrdinaryApplyRequiresPhysicalCodecOnlyForChangedState(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "no-op"
		if changed {
			name = "changed"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newApplyEventFixture(t)
			var actions []applyEventAction
			if changed {
				actions = []applyEventAction{fixture.createAction("create", "CREATE.md", "created\n")}
			}
			input := fixture.input(actions)
			input.StatefileCodec = statefile.PhysicalCodec{}
			result, err := ApplyWithOptions(t.Context(), input, ApplyOptions{})
			if (err != nil) != changed || result.ExecutionAttempted {
				t.Fatalf("changed=%t: unexpected execution or codec admission: %v", changed, err)
			}
			assertHostMissing(t, fixture.hostPath("CREATE.md"))
			assertHostMissing(t, fixture.paths.StatefilePath)
			assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
		})
	}
}

func TestAttemptCheckpointsKeepSelectedPhysicalStateEncoding(t *testing.T) {
	owner := testAuthority(t)
	action := testInvokingActionAttempt(t)
	codec := stateimage.NewCodec(t, owner.StatefileKey())
	var content []byte
	commit := func(_ context.Context, next []byte, _ os.FileMode) error {
		content = next
		return nil
	}
	current, err := commitPendingCarrierInstalls(t.Context(), commit, durable.EmptySnapshot(), owner, []reconciliation.RelationAction{action}, codec)
	if err != nil {
		t.Fatal(err)
	}
	image, err := statefile.UnmarshalImage(content)
	if err != nil || !image.Snapshot().Equal(current) {
		t.Fatal("write-ahead checkpoint did not use the selected State encoder", err)
	}
	current, err = commitRetiredPendingCarrierInstall(t.Context(), commit, current, owner, action, codec)
	if err != nil {
		t.Fatal(err)
	}
	image, err = statefile.UnmarshalImage(content)
	if err != nil || !image.Snapshot().Equal(current) || len(current.PendingCarrierInstalls()) != 0 {
		t.Fatal("checkpoint retirement changed the physical State representation", err)
	}
}
