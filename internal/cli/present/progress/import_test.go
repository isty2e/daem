package progress_test

import (
	"bytes"
	"strings"
	"testing"

	cliprogress "github.com/isty2e/daem/internal/cli/present/progress"
	"github.com/isty2e/daem/internal/target"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

func TestImportProgressRendererShowsProvisionalPhasesAndClears(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewImportProgressRenderer(&output)
	sink := renderer.Sink()
	for _, event := range []workflowadopt.ProgressEvent{
		{Kind: workflowadopt.ProgressEventPhaseStarted, Phase: workflowadopt.ProgressPhaseDiscovery, Total: 2},
		{Kind: workflowadopt.ProgressEventTargetScopeStarted, Phase: workflowadopt.ProgressPhaseDiscovery, Target: target.TargetCodex, Scope: target.ScopeProject, Total: 2},
		{Kind: workflowadopt.ProgressEventTargetScopeCompleted, Phase: workflowadopt.ProgressPhaseDiscovery, Target: target.TargetCodex, Scope: target.ScopeProject, Completed: 1, Total: 2},
		{Kind: workflowadopt.ProgressEventPhaseStarted, Phase: workflowadopt.ProgressPhaseRevalidation, Total: 2},
		{Kind: workflowadopt.ProgressEventTargetScopeStarted, Phase: workflowadopt.ProgressPhaseRevalidation, Target: target.TargetClaudeCode, Scope: target.ScopeGlobal, Completed: 1, Total: 2},
		{Kind: workflowadopt.ProgressEventPhaseCompleted, Phase: workflowadopt.ProgressPhaseRevalidation, Completed: 2, Total: 2},
		{Kind: workflowadopt.ProgressEventPhaseStarted, Phase: workflowadopt.ProgressPhasePublication},
		{Kind: workflowadopt.ProgressEventPhaseCompleted, Phase: workflowadopt.ProgressPhasePublication},
	} {
		sink(event)
	}
	renderer.Close()

	got := output.String()
	for _, want := range []string{
		"Discovering import candidates",
		"Discovering import candidates 0/2: codex/project",
		"Discovering import candidates 1/2: codex/project",
		"Revalidating import sources",
		"Revalidating import candidates 1/2: claude-code/global",
		"Publishing import changes",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	if strings.Count(got, "\n") != 0 || !strings.HasSuffix(got, "\r\x1b[2K") {
		t.Fatalf("output = %q, want cleared ephemeral lines", got)
	}
}

func TestImportProgressRendererSuppressesAfterWriteError(t *testing.T) {
	writer := &failAfterFirstWrite{}
	renderer := cliprogress.NewImportProgressRenderer(writer)
	renderer.Sink()(workflowadopt.ProgressEvent{
		Kind:  workflowadopt.ProgressEventPhaseStarted,
		Phase: workflowadopt.ProgressPhaseDiscovery,
		Total: 1,
	})
	renderer.Sink()(workflowadopt.ProgressEvent{
		Kind:   workflowadopt.ProgressEventTargetScopeStarted,
		Phase:  workflowadopt.ProgressPhaseDiscovery,
		Target: target.TargetCodex,
		Scope:  target.ScopeProject,
		Total:  1,
	})
	renderer.Sink()(workflowadopt.ProgressEvent{
		Kind:      workflowadopt.ProgressEventTargetScopeCompleted,
		Phase:     workflowadopt.ProgressPhaseDiscovery,
		Target:    target.TargetCodex,
		Scope:     target.ScopeProject,
		Completed: 1,
		Total:     1,
	})
	renderer.Close()
	if writer.writeAttempts != 2 {
		t.Fatalf("write attempts = %d, want 2", writer.writeAttempts)
	}
}

func TestImportProgressRendererRejectsInvalidFacts(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewImportProgressRenderer(&output)
	renderer.Sink()(workflowadopt.ProgressEvent{
		Kind:   workflowadopt.ProgressEventTargetScopeStarted,
		Phase:  workflowadopt.ProgressPhaseDiscovery,
		Target: target.Target("bad\n\x1b[31m"),
		Scope:  target.ScopeProject,
		Total:  1,
	})
	renderer.Close()
	if output.Len() != 0 {
		t.Fatalf("output = %q, want invalid fact suppressed", output.String())
	}
}

func TestImportProgressRendererNilReceiverIsNoop(t *testing.T) {
	var renderer *cliprogress.ImportProgressRenderer
	if renderer.Sink() != nil {
		t.Fatal("nil renderer returned non-nil sink")
	}
	renderer.Close()
}
