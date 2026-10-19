package clipresent

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/reconcile"
)

func TestPrintRelationActionSummaryPreservesPlannedEffects(t *testing.T) {
	record, relation := claudePluginCarrierFixture(t)
	expected := relation.ExpectedRelation()
	exact := observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			claudePluginManagedRow(t, string(expected.SubjectKey()), string(expected.ManagedInstanceKey())),
		},
	}
	missing := observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}
	unsupported := observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnsupported,
		Freshness:    observerelation.EvidenceFresh,
	}
	unavailable := observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnavailable,
		Freshness:    observerelation.EvidenceFresh,
	}
	stale := exact
	stale.Freshness = observerelation.EvidenceStale

	tests := []struct {
		name      string
		action    reconcile.RelationAction
		kind      reconcile.RelationActionKind
		execution reconcile.RelationExecutionClass
		label     string
		reason    string
	}{
		{
			name:      "no-op",
			action:    claudePluginCarrierActionForSubject(t, record, relation, exact),
			kind:      reconcile.ActionNoOp,
			execution: reconcile.ExecutionNoMutation,
			label:     "no change planned for extension relation",
		},
		{
			name:      "observe unsupported inventory",
			action:    claudePluginCarrierAction(t, unsupported),
			kind:      reconcile.ActionObserveOnly,
			execution: reconcile.ExecutionObserveOnly,
			label:     "observe extension relation only",
			reason:    "unsupported passive inventory",
		},
		{
			name:      "observe unavailable inventory",
			action:    claudePluginCarrierAction(t, unavailable),
			kind:      reconcile.ActionObserveOnly,
			execution: reconcile.ExecutionObserveOnly,
			label:     "observe extension relation only",
			reason:    "relation evidence unavailable",
		},
		{
			name:      "observe missing relation",
			action:    claudePluginCarrierActionWithSelectedOutcome(t, missing, reconcile.AdmissionOutcomeObserveOnly),
			kind:      reconcile.ActionObserveOnly,
			execution: reconcile.ExecutionObserveOnly,
			label:     "observe extension relation only",
			reason:    "unsupported passive inventory",
		},
		{
			name:      "create through host",
			action:    claudePluginCarrierActionWithSelectedOutcome(t, missing, reconcile.AdmissionOutcomeHostDelegated),
			kind:      reconcile.ActionCreate,
			execution: reconcile.ExecutionHostRoute,
			label:     "install extension through host",
		},
		{
			name:      "create blocked",
			action:    claudePluginCarrierAction(t, missing),
			kind:      reconcile.ActionCreate,
			execution: reconcile.ExecutionBlocked,
			label:     "blocked",
			reason:    "route not admitted",
		},
		{
			name:      "assistance required",
			action:    claudePluginCarrierActionWithSelectedOutcome(t, missing, reconcile.AdmissionOutcomeAssisted),
			kind:      reconcile.ActionAssistCandidate,
			execution: reconcile.ExecutionAssisted,
			label:     "blocked",
			reason:    "route requires assistance",
		},
		{
			name:      "present unclaimed",
			action:    claudePluginCarrierActionForSubjectWithClaimPresence(t, record, relation, exact, reconcile.AdmissionOutcomeHostDelegated, false),
			kind:      reconcile.ActionBlock,
			execution: reconcile.ExecutionBlocked,
			label:     "external carrier present but unclaimed",
		},
		{
			name:      "stale evidence blocked",
			action:    claudePluginCarrierAction(t, stale),
			kind:      reconcile.ActionBlock,
			execution: reconcile.ExecutionBlocked,
			label:     "blocked",
			reason:    "stale evidence",
		},
		{
			name:      "attempt through host",
			action:    codexNoObserverAttemptAction(t),
			kind:      reconcile.ActionAttempt,
			execution: reconcile.ExecutionHostRoute,
			label:     "install extension through host",
		},
	}

	actions := make([]reconcile.RelationAction, 0, len(tests))
	for _, test := range tests {
		actions = append(actions, test.action)
		t.Run(test.name, func(t *testing.T) {
			if test.action.Kind() != test.kind || test.action.Execution() != test.execution {
				t.Fatalf("fixture kind=%s execution=%s, want %s/%s", test.action.Kind(), test.action.Execution(), test.kind, test.execution)
			}

			var output bytes.Buffer
			PrintRelationActionsWithOptions(&output, []reconcile.RelationAction{test.action}, HumanOptions{})
			text := output.String()
			if test.kind == reconcile.ActionNoOp {
				if text != "relation actions: 1 entry (1 unchanged)\n" {
					t.Fatalf("no-op summary = %q", text)
				}
				output.Reset()
				PrintRelationActionsWithOptions(&output, []reconcile.RelationAction{test.action}, HumanOptions{Verbose: true})
				if !strings.Contains(output.String(), "kind="+string(reconcile.ActionNoOp)) || strings.Count(output.String(), "  - ") != 1 {
					t.Fatalf("verbose lost the no-op fact: %s", output.String())
				}
				return
			}
			identity := fmt.Sprintf("resource=%q target=%s scope=%s", "extension/"+test.action.Subject().Key(), test.action.Target(), test.action.Scope())
			row := "  - " + test.label + " " + identity
			if test.reason != "" {
				row += ": " + test.reason
			}
			if !strings.Contains(text, row+"\n") {
				t.Errorf("summary = %q, want row %q", text, row)
			}
			if !strings.HasPrefix(text, "relation actions: 1 entry\n") || strings.Count(text, "  - ") != 1 {
				t.Errorf("summary lost subject count: %q", text)
			}
			warning := "host may retain packages, caches, credentials, trust data, or logs"
			if strings.Contains(text, warning) != test.action.InvokesHostRoute() {
				t.Errorf("summary retained-effects disclosure disagrees with invocation: %q", text)
			}
			for _, forbidden := range []string{"remove", "installed", "converged", "success", "ready"} {
				if strings.Contains(text, forbidden) {
					t.Errorf("summary invents %q: %q", forbidden, text)
				}
			}
		})
	}

	var output bytes.Buffer
	PrintRelationActionsWithOptions(&output, actions, HumanOptions{})
	if !strings.HasPrefix(output.String(), fmt.Sprintf("relation actions: %d entries (1 unchanged)\n", len(actions))) || strings.Count(output.String(), "  - ") != len(actions)-1 {
		t.Fatalf("summary row/header count differs from input actions: %q", output.String())
	}
}

func TestPrintRelationActionSummaryDoesNotInventUnhandledEffects(t *testing.T) {
	var output bytes.Buffer
	PrintRelationActionsWithOptions(&output, []reconcile.RelationAction{{}}, HumanOptions{})
	text := output.String()
	if !strings.Contains(text, `relation action kind="" subject="" target= scope=`) {
		t.Errorf("summary lost unhandled kind: %q", text)
	}
	for _, forbidden := range []string{"remove", "install", "change planned", "blocked"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("summary invents %q for unhandled kind: %q", forbidden, text)
		}
	}
}
