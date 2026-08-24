package adopt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAndExecuteCommandPlanEmitBoundedImportProgress(t *testing.T) {
	root := importProgressFixture(t)
	output := filepath.Join(root, "daem.toml")

	var discovery []ProgressEvent
	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex", "claude-code"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
		ProgressEvents: func(event ProgressEvent) {
			discovery = append(discovery, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertImportProgressEvents(t, discovery, ProgressPhaseDiscovery, 2, false)
	if discovery[1].Target != "codex" || discovery[3].Target != "claude-code" {
		t.Fatalf("target order = %q/%q", discovery[1].Target, discovery[3].Target)
	}

	var execution []ProgressEvent
	if _, err := ExecuteCommandPlan(t.Context(), planned, func(event ProgressEvent) {
		execution = append(execution, event)
	}); err != nil {
		t.Fatal(err)
	}
	assertImportProgressEvents(t, execution, ProgressPhaseRevalidation, 2, true)
}

func TestImportProgressDoesNotAnnouncePublicationAfterFreshnessFailure(t *testing.T) {
	root := importProgressFixture(t)
	output := filepath.Join(root, "daem.toml")
	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("AGENTS.md", []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var events []ProgressEvent
	_, err = ExecuteCommandPlan(t.Context(), planned, func(event ProgressEvent) {
		events = append(events, event)
	})
	if err == nil {
		t.Fatal("ExecuteCommandPlan succeeded after source drift")
	}
	for _, event := range events {
		if event.Phase == ProgressPhasePublication {
			t.Fatalf("publication event after freshness failure: %#v", event)
		}
	}
}

func TestImportProgressEmitsNothingForPreCanceledCalls(t *testing.T) {
	root := importProgressFixture(t)
	output := filepath.Join(root, "daem.toml")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var events []ProgressEvent
	_, err := BuildCommandPlan(ctx, CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
		ProgressEvents: func(event ProgressEvent) {
			events = append(events, event)
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildCommandPlan error = %v, want context.Canceled", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v, want none", events)
	}

	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	events = nil
	_, err = ExecuteCommandPlan(ctx, planned, func(event ProgressEvent) {
		events = append(events, event)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteCommandPlan error = %v, want context.Canceled", err)
	}
	if len(events) != 0 {
		t.Fatalf("execution events = %#v, want none", events)
	}
}

func importProgressFixture(t *testing.T) string {
	t.Helper()
	root := enterAdoptTestDirectory(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "xdg-state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	if err := os.WriteFile("AGENTS.md", []byte("# Agents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("CLAUDE.md", []byte("# Claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertImportProgressEvents(
	t *testing.T,
	events []ProgressEvent,
	phase ProgressPhase,
	total int,
	wantPublication bool,
) {
	t.Helper()
	wantCount := 2 + 2*total
	if wantPublication {
		wantCount += 2
	}
	if len(events) != wantCount {
		t.Fatalf("events = %#v, want %d", events, wantCount)
	}
	if events[0] != (ProgressEvent{Kind: ProgressEventPhaseStarted, Phase: phase, Total: total}) {
		t.Fatalf("first event = %#v", events[0])
	}
	for index := range total {
		started := events[1+2*index]
		completed := events[2+2*index]
		if started.Kind != ProgressEventTargetScopeStarted ||
			started.Phase != phase ||
			started.Completed != index ||
			started.Total != total ||
			completed.Kind != ProgressEventTargetScopeCompleted ||
			completed.Phase != phase ||
			completed.Completed != index+1 ||
			completed.Total != total ||
			started.Target != completed.Target ||
			started.Scope != completed.Scope {
			t.Fatalf("target/scope events %d = %#v / %#v", index, started, completed)
		}
	}
	phaseCompleted := events[1+2*total]
	if phaseCompleted != (ProgressEvent{
		Kind:      ProgressEventPhaseCompleted,
		Phase:     phase,
		Completed: total,
		Total:     total,
	}) {
		t.Fatalf("phase completion = %#v", phaseCompleted)
	}
	if !wantPublication {
		return
	}
	if events[len(events)-2] != (ProgressEvent{
		Kind:  ProgressEventPhaseStarted,
		Phase: ProgressPhasePublication,
	}) || events[len(events)-1] != (ProgressEvent{
		Kind:  ProgressEventPhaseCompleted,
		Phase: ProgressPhasePublication,
	}) {
		t.Fatalf("publication events = %#v", events[len(events)-2:])
	}
}
