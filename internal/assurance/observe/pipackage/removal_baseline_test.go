package pipackage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/target"
)

func TestCaptureRemovalBaselinesOwnsSourceClassRequirements(t *testing.T) {
	root := resolvedTempDir(t)
	localSource := filepath.Join(root, "local-extension")
	localCarrier := mustPiCarrierKey(t, target.ScopeProject, localSource)
	localRequirements := mustEffectRequirements(t, effectpostcondition.LocalSourceUnchanged)

	absent, err := CaptureRemovalBaselines(context.Background(), RemovalBaselineInput{
		CommandRoot:          root,
		Carrier:              localCarrier,
		EffectPostconditions: localRequirements,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBaselineState(t, absent, durablecarrier.EffectBaselineAbsent)

	if err := os.MkdirAll(localSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSource, "index.ts"), []byte("export default 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content, err := CaptureRemovalBaselines(context.Background(), RemovalBaselineInput{
		CommandRoot:          root,
		Carrier:              localCarrier,
		EffectPostconditions: localRequirements,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBaselineState(t, content, durablecarrier.EffectBaselineContent)
	baseline, _ := content.For(effectpostcondition.LocalSourceUnchanged)
	if _, present := baseline.ContentHash(); !present {
		t.Fatal("content baseline has no content hash")
	}

	npm, err := CaptureRemovalBaselines(context.Background(), RemovalBaselineInput{
		CommandRoot:          root,
		Carrier:              mustPiCarrierKey(t, target.ScopeProject, "npm:@acme/tools@1.2.3"),
		EffectPostconditions: mustEffectRequirements(t, effectpostcondition.CarrierArtifactsAbsent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(npm.Baselines()) != 0 {
		t.Fatalf("npm baselines = %#v, want empty", npm.Baselines())
	}

	if _, err := CaptureRemovalBaselines(nil, RemovalBaselineInput{
		CommandRoot:          root,
		Carrier:              localCarrier,
		EffectPostconditions: localRequirements,
	}); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil context error = %v, want required-context error", err)
	}
}

func TestObserveLocalRemovalComparesImmutableBaseline(t *testing.T) {
	tests := []struct {
		name      string
		before    *string
		after     *string
		wantState observepostcondition.EvidenceState
	}{
		{
			name:      "absent remains absent",
			wantState: observepostcondition.EvidenceSatisfied,
		},
		{
			name:      "absent becomes present",
			after:     pointerTo("created\n"),
			wantState: observepostcondition.EvidenceUnsatisfied,
		},
		{
			name:      "content remains equal",
			before:    pointerTo("same\n"),
			after:     pointerTo("same\n"),
			wantState: observepostcondition.EvidenceSatisfied,
		},
		{
			name:      "content mutates",
			before:    pointerTo("before\n"),
			after:     pointerTo("after\n"),
			wantState: observepostcondition.EvidenceUnsatisfied,
		},
		{
			name:      "content disappears",
			before:    pointerTo("before\n"),
			wantState: observepostcondition.EvidenceUnsatisfied,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := resolvedTempDir(t)
			sourcePath := filepath.Join(root, "extension")
			if test.before != nil {
				writeSourceContent(t, sourcePath, *test.before)
			}
			carrier := mustPiCarrierKey(t, target.ScopeProject, sourcePath)
			requirements := mustEffectRequirements(t, effectpostcondition.LocalSourceUnchanged)
			baselines, err := CaptureRemovalBaselines(context.Background(), RemovalBaselineInput{
				CommandRoot:          root,
				Carrier:              carrier,
				EffectPostconditions: requirements,
			})
			if err != nil {
				t.Fatal(err)
			}

			if err := os.RemoveAll(sourcePath); err != nil {
				t.Fatal(err)
			}
			if test.after != nil {
				writeSourceContent(t, sourcePath, *test.after)
			}
			pending := mustPiPendingRemoval(t, root, target.ScopeProject, sourcePath, baselines)
			state, err := observeLocalSourceUnchanged(context.Background(), root, carrier, pending)
			if err != nil {
				t.Fatal(err)
			}
			if state != test.wantState {
				t.Fatalf("state = %q, want %q", state, test.wantState)
			}
		})
	}
}

func assertBaselineState(
	t *testing.T,
	set durablecarrier.EffectBaselineSet,
	want durablecarrier.EffectBaselineState,
) {
	t.Helper()
	baseline, present := set.For(effectpostcondition.LocalSourceUnchanged)
	if !present || baseline.State() != want {
		t.Fatalf("baseline = (%#v, %t), want %q", baseline, present, want)
	}
}
