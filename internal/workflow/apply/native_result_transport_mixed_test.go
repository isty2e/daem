package apply_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestNativeResultTransportPreservesOrdinaryProjectionOutput(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	if err := os.WriteFile(manifestPath, []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatal(err)
	}
	planned, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextDryRun, Aggregates: planned.Reconciliation.Aggregates()})
	if err != nil || ordinary.ProjectionDecisionCount() == 0 {
		t.Fatal("ordinary fixture has no projected work", err)
	}
	if err := execute.RejectUnsupportedExecutableActions(ordinary); err != nil {
		t.Fatal("ordinary supported work was blocked", err)
	}
	action := nativeTransportAction(t, "settings", `{}`)
	mixed, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextDryRun, Aggregates: ordinary.Aggregates(), NativeNarrowings: []reconcile.NativeNarrowingAction{action}})
	if err != nil || mixed.DecisionCount() != ordinary.DecisionCount()+1 || mixed.ProjectionDecisionCount() != ordinary.ProjectionDecisionCount() {
		t.Fatal("native transport changed ordinary count semantics", err)
	}
	var envelopes []map[string]json.RawMessage
	for _, result := range []reconcile.Result{ordinary, mixed} {
		var content bytes.Buffer
		if err := clipresent.PrintPlanJSON(&content, clipresent.PlanJSONInput{Command: "apply", Mode: "dry-run", Reconciliation: result}); err != nil {
			t.Fatal(err)
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(content.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, envelope)
	}
	if _, present := envelopes[0]["native_narrowing_actions"]; present {
		t.Fatal("ordinary output acquired an empty native action family")
	}
	delete(envelopes[1], "native_narrowing_actions")
	if len(envelopes[0]) != len(envelopes[1]) {
		t.Fatal("native transport added unrelated ordinary fields")
	}
	for key, value := range envelopes[0] {
		if !bytes.Equal(value, envelopes[1][key]) {
			t.Fatalf("native transport changed ordinary field %q", key)
		}
	}
}

func TestNativeResultTransportRejectsCompetingKeyDecision(t *testing.T) {
	action := nativeTransportAction(t, "settings", `{"private-key-marker":"actual-secret-marker"}`)
	plan, present := action.KeyPlan()
	if !present || len(action.DesiredContributions()) != 1 || len(plan.Intents()) != 1 {
		t.Fatal("fixture has no complete single-key narrowing plan")
	}
	item := action.DesiredContributions()[0]
	contract := item.Contribution().Contract()
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{contract.CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := catalog.Lookup(contract.CodecContractID())
	rendered, failure := codec.Render(action.CurrentDocument(), plan)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(action.CurrentDocument(), plan.Before(), aggregate.DocumentFileMode)
	if err != nil {
		t.Fatal(err)
	}
	desired, _ := plan.Intents()[0].Desired()
	expected, _ := rendered.Expected().State(contract)
	selection, err := plan.Before().Selection()
	if err != nil {
		t.Fatal(err)
	}
	operation, err := profile.NewNativeAggregateOperationContract(catalog, selection)
	if err != nil {
		t.Fatal(err)
	}
	keyRecord, err := reconcile.NewAggregateDecision(reconcile.AggregateDecisionInput{
		Kind: reconcile.AggregateRecord, Reason: reconcile.ReasonManagedExisting, DocumentAddress: contract.Address().Document(), CodecContractID: contract.CodecContractID(),
		Document: action.CurrentDocument(), Snapshot: plan.Before(), CodecPlan: plan, Rendered: rendered, Evidence: evidence,
		NativeOperation: &operation,
		Projections: []reconcile.AggregateProjectionDecisionInput{{
			Kind: reconcile.AggregateRecord, Reason: reconcile.ReasonManagedExisting, Contract: contract, Desired: &desired, Before: plan.Intents()[0].Before(), Expected: expected,
			Subjects: []reconcile.AggregateSubjectDecisionInput{{Subject: item.SubjectID(), Contract: contract, Kind: reconcile.AggregateRecord, Reason: reconcile.ReasonManagedExisting}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, NativeNarrowings: []reconcile.NativeNarrowingAction{action}, Aggregates: []reconcile.AggregateDecision{keyRecord}}); err == nil || result.DecisionCount() != 0 {
		t.Fatal("narrowing and an ordinary key decision both claimed the same subject")
	}
}
