package progress_test

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	cliprogress "github.com/isty2e/daem/internal/cli/present/progress"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/execute"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestApplyProgressRendererShowsOnlyEphemeralActionProgress(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewApplyProgressRenderer(cliprogress.ApplyProgressRendererOptions{Output: &output})
	sink := renderer.Sink()
	sink(execute.Event{Kind: execute.EventJournalCaptureStarted, TotalActions: 2})
	sink(execute.Event{Kind: execute.EventActionStarted, TotalActions: 2, Action: applyProgressActionFacts(t)})
	sink(execute.Event{Kind: execute.EventActionDone, TotalActions: 2, Action: applyProgressActionFacts(t)})
	renderer.Close()

	got := output.String()
	for _, want := range []string{"\r\x1b[2KApplying 0/2: hook/project -> .codex/hooks.json", "\r\x1b[2KApplying 1/2", "\r\x1b[2K"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	for _, forbidden := range []string{"journal", "stage=", "action_index", "reason=", "write destination failed"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("output = %q, did not want %q", got, forbidden)
		}
	}
}

func TestApplyProgressRendererSuppressesUntrustedErrors(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewApplyProgressRenderer(cliprogress.ApplyProgressRendererOptions{Output: &output})
	facts := applyProgressActionFacts(t)
	renderer.Sink()(execute.Event{Kind: execute.EventActionFailed, TotalActions: 1, Action: facts, Err: errors.New("secret\nerror")})

	got := output.String()
	if strings.Contains(got, "secret") {
		t.Fatalf("output leaked error text: %q", got)
	}
	if !strings.Contains(got, ".codex/hooks.json") || !strings.Contains(got, ": failed") {
		t.Fatalf("output = %q, want canonical label and failure state", got)
	}
}

func TestApplyProgressRendererReportsPhysicalOrderSequenceOutcomes(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewApplyProgressRenderer(
		cliprogress.ApplyProgressRendererOptions{Output: &output},
	)
	sink := renderer.Sink()
	sequence, err := hostrelation.NewPhysicalSequenceID(
		"opencode:project:server.plugins",
	)
	if err != nil {
		t.Fatal(err)
	}
	facts := &execute.RelationOrderEventFacts{
		Target: target.TargetOpenCode, Scope: target.ScopeProject,
		SequenceID: sequence,
	}
	sink(execute.Event{
		Kind: execute.EventRelationOrderStarted, RelationOrder: facts,
	})
	facts.Changed = true
	sink(execute.Event{
		Kind: execute.EventRelationOrderDone, RelationOrder: facts,
	})
	facts.Changed = false
	sink(execute.Event{
		Kind:          execute.EventRelationOrderFailed,
		RelationOrder: facts,
		Err:           errors.New("secret host path"),
	})
	renderer.Close()

	got := output.String()
	for _, want := range []string{
		"Applying extension order: opencode project opencode:project:server.plugins",
		": converged",
		": failed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "secret host path") || strings.Contains(got, "%") {
		t.Fatalf("output leaked details or invented percentage: %q", got)
	}
}

func TestApplyProgressRendererAttributesEntityBackedManagedPath(t *testing.T) {
	entityID, err := entity.New(entity.KindSkill, "oracle")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(entityID, "skill.project.agents")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	renderer := cliprogress.NewApplyProgressRenderer(cliprogress.ApplyProgressRendererOptions{Output: &output})
	renderer.Sink()(execute.Event{
		Kind: execute.EventActionStarted, TotalActions: 1,
		Action: &execute.ActionEventFacts{
			Index: 0, ManagedPathKind: execute.ManagedPathEffectCreate, Subject: subject,
			ConsumerTargets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
			Destination: outputtest.Parse(t, ".agents/skills/oracle"),
		},
	})
	if !strings.Contains(output.String(), "skill/oracle -> .agents/skills/oracle") {
		t.Fatalf("managed path progress = %q, want resource attribution", output.String())
	}
}

func TestApplyProgressRendererSuppressesAfterWriteError(t *testing.T) {
	writer := &applyFailAfterFirstWrite{}
	renderer := cliprogress.NewApplyProgressRenderer(cliprogress.ApplyProgressRendererOptions{Output: writer})
	renderer.Sink()(execute.Event{Kind: execute.EventActionStarted, TotalActions: 2, Action: applyProgressActionFacts(t)})
	renderer.Sink()(execute.Event{Kind: execute.EventActionDone, TotalActions: 2, Action: applyProgressActionFacts(t)})
	renderer.Close()
	if writer.writes != 2 {
		t.Fatalf("writes = %d, want 2", writer.writes)
	}
}

func TestApplyProgressRendererDoesNotDoubleCountRepeatedCompletion(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewApplyProgressRenderer(cliprogress.ApplyProgressRendererOptions{Output: &output})
	event := execute.Event{Kind: execute.EventActionDone, TotalActions: 2, Action: applyProgressActionFacts(t)}
	renderer.Sink()(event)
	renderer.Sink()(event)
	if strings.Contains(output.String(), "Applying 2/2") || strings.Count(output.String(), "Applying 1/2") != 1 {
		t.Fatalf("output = %q, want one idempotent completion", output.String())
	}
}

func TestApplyProgressRendererAcceptsConcurrentEvents(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewApplyProgressRenderer(cliprogress.ApplyProgressRendererOptions{Output: &output})
	var waitGroup sync.WaitGroup
	for range 32 {
		waitGroup.Go(func() {
			renderer.Sink()(execute.Event{Kind: execute.EventActionStarted, TotalActions: 32, Action: applyProgressActionFacts(t)})
		})
	}
	waitGroup.Wait()
	if !strings.Contains(output.String(), "Applying 0/32") {
		t.Fatalf("output = %q, want progress", output.String())
	}
}

func TestApplyProgressRendererNilReceiverIsNoop(t *testing.T) {
	var renderer *cliprogress.ApplyProgressRenderer
	if sink := renderer.Sink(); sink != nil {
		t.Fatalf("nil renderer Sink returned non-nil sink")
	}
	renderer.Close()
}

func applyProgressActionFacts(t testing.TB) *execute.ActionEventFacts {
	entityID, err := entity.New(entity.KindHook, "project")
	if err != nil {
		panic(err)
	}
	subject, err := topologyprojection.Subject(entityID, "hook.project.codex")
	if err != nil {
		panic(err)
	}
	return &execute.ActionEventFacts{
		Index:           1,
		ManagedPathKind: execute.ManagedPathEffectCreate,
		Subject:         subject,
		Target:          target.TargetCodex,
		Scope:           target.ScopeProject,
		Destination:     outputtest.Parse(t, ".codex/hooks.json"),
	}
}

type applyFailAfterFirstWrite struct {
	buffer bytes.Buffer
	writes int
}

func (writer *applyFailAfterFirstWrite) Write(content []byte) (int, error) {
	writer.writes++
	if writer.writes > 1 {
		return 0, errors.New("stderr closed")
	}
	return writer.buffer.Write(content)
}
