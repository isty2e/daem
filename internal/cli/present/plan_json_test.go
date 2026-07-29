package clipresent

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/findings"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

type planJSONTestPayload struct {
	HasErrors bool `json:"has_errors"`
	Actions   []struct {
		Targets       []string `json:"targets"`
		PreviousState *struct {
			Targets []string `json:"targets"`
		} `json:"previous_state"`
	} `json:"actions"`
}

type applyResultJSONTestPayload struct {
	HasErrors bool `json:"has_errors"`
	Actions   []struct {
		Subject *struct {
			Kind      string `json:"kind"`
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"subject"`
		ResourceID string `json:"resource_id"`
		Projection *struct {
			Target      string `json:"target"`
			Scope       string `json:"scope"`
			ConfigPath  string `json:"config_path"`
			ContentPath string `json:"content_path"`
		} `json:"projection"`
	} `json:"actions"`
}

func planTestEntityID(t *testing.T, name string) entity.ID {
	t.Helper()
	id, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	return id
}

func TestPrintPlanJSONTreatsErrorDiagnosticsAsErrors(t *testing.T) {
	var stdout bytes.Buffer
	err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command: "status",
		Mode:    "status",
		Diagnostics: []findings.Diagnostic{
			{
				Severity: findings.SeverityError,
				Code:     "skill.compat.manual",
				EntityID: planTestEntityID(t, "oracle"),
				Target:   target.TargetOpenCode,
				Scope:    target.ScopeGlobal,
				Detail:   "skill compatibility failure requires manual source edits",
			},
		},
	})
	if err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	var payload planJSONTestPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan json: %v", err)
	}
	if !payload.HasErrors {
		t.Fatalf("HasErrors = false, payload = %#v", payload)
	}
}

func TestPlanJSONPreservesExplicitFileModeZero(t *testing.T) {
	content, err := json.Marshal(planJSONAction{
		Kind:            "create",
		DesiredFileMode: fileModeJSONPointer(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte(`"desired_file_mode":0`)) {
		t.Fatalf("plan JSON omitted explicit file mode zero: %s", content)
	}
}

func TestPrintApplyResultJSONTreatsErrorDiagnosticsAsErrors(t *testing.T) {
	var stdout bytes.Buffer
	err := PrintApplyResultJSON(&stdout, ApplyResultJSONInput{
		StatefilePath: "/tmp/state.json",
		Diagnostics: []findings.Diagnostic{
			{
				Severity: findings.SeverityError,
				Code:     "skill.compat.manual",
				EntityID: planTestEntityID(t, "oracle"),
				Target:   target.TargetOpenCode,
				Scope:    target.ScopeGlobal,
				Detail:   "skill compatibility failure requires manual source edits",
			},
		},
	})
	if err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}

	var payload applyResultJSONTestPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode apply result json: %v", err)
	}
	if !payload.HasErrors {
		t.Fatalf("HasErrors = false, payload = %#v", payload)
	}
}

func TestPrintPlanJSONIncludesRemainingAndPreviousConsumerTargets(t *testing.T) {
	fixture := newManagedPathPlanFixture(
		t,
		"oracle",
		"oracle",
		target.ScopeProject,
		[]target.Target{target.TargetAntigravityCLI, target.TargetCodex},
		"desired",
	)
	planResult := fixture.buildPlan(t, managedPathPlanInput{
		selectedTargets: []target.Target{target.TargetCodex},
		states: []durable.ManagedPathState{
			fixture.state(t, []target.Target{target.TargetAntigravityCLI, target.TargetCodex}, "old"),
		},
		evidence: []observe.ManagedPathEvidence{fixture.evidence(t, true, "old")},
	})

	var stdout bytes.Buffer
	err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command:        "status",
		Mode:           "status",
		Reconciliation: planResult,
	})
	if err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	var payload planJSONTestPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan json: %v", err)
	}
	if len(payload.Actions) != 1 {
		t.Fatalf("actions = %#v, want one action", payload.Actions)
	}
	if strings.Join(payload.Actions[0].Targets, ",") != "antigravity-cli" {
		t.Fatalf("targets = %#v", payload.Actions[0].Targets)
	}
	if payload.Actions[0].PreviousState == nil ||
		strings.Join(payload.Actions[0].PreviousState.Targets, ",") != "antigravity-cli,codex" {
		t.Fatalf("previous_state = %#v", payload.Actions[0].PreviousState)
	}
}

func TestPrintPlanJSONIncludesSubjectProjectionActions(t *testing.T) {
	var stdout bytes.Buffer
	err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command:        "apply",
		Mode:           "dry-run",
		Reconciliation: mcpProjectionPlan(t),
	})
	if err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	var payload applyResultJSONTestPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan json: %v", err)
	}
	if len(payload.Actions) != 1 {
		t.Fatalf("actions = %#v, want one", payload.Actions)
	}
	action := payload.Actions[0]
	if action.ResourceID != "" {
		t.Fatalf("resource_id = %q, want omitted for subject action", action.ResourceID)
	}
	if action.Subject == nil ||
		action.Subject.Kind != "projection" ||
		action.Subject.Namespace != "claude-code.project.mcp-server" ||
		action.Subject.Name != "context7" {
		t.Fatalf("subject = %#v", action.Subject)
	}
	if action.Projection == nil ||
		action.Projection.Target != string(target.TargetClaudeCode) ||
		action.Projection.Scope != string(target.ScopeProject) ||
		action.Projection.ConfigPath != ".mcp.json" ||
		action.Projection.ContentPath != "/mcpServers/context7" {
		t.Fatalf("projection = %#v", action.Projection)
	}
}

func TestPrintPlanJSONIncludesMCPRemovalDetail(t *testing.T) {
	detail := "managed MCP config entry will be removed; runtime absence is not claimed"
	var stdout bytes.Buffer
	err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command:        "apply",
		Mode:           "dry-run",
		Reconciliation: mcpProjectionRemovalPlan(t, detail),
	})
	if err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	var payload struct {
		Actions []struct {
			Kind   string `json:"kind"`
			Detail string `json:"detail"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan json: %v", err)
	}
	if len(payload.Actions) != 1 ||
		payload.Actions[0].Kind != "delete" ||
		payload.Actions[0].Detail != detail {
		t.Fatalf("actions = %#v, want one detailed delete", payload.Actions)
	}
}

func TestPrintPlanJSONIncludesRelationActions(t *testing.T) {
	action := claudePluginCarrierAction(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})

	var stdout bytes.Buffer
	err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command: "apply",
		Mode:    "dry-run",
		Reconciliation: reconciliationWithFamilies(
			t, reconciliation.ContextDryRun, reconciliation.Result{},
			[]reconciliation.RelationAction{action}, nil,
		),
	})
	if err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	var payload struct {
		HasErrors       bool       `json:"has_errors"`
		Actions         []struct{} `json:"actions"`
		DelegateActions []struct{} `json:"delegate_actions"`
		RelationActions []struct {
			Kind    string `json:"kind"`
			Subject *struct {
				Kind      string `json:"kind"`
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"subject"`
			Target               string   `json:"target"`
			Scope                string   `json:"scope"`
			RelationSubjectKey   string   `json:"relation_subject_key"`
			EvidenceSource       string   `json:"evidence_source"`
			EvidenceAvailability string   `json:"evidence_availability"`
			EvidenceFreshness    string   `json:"evidence_freshness"`
			RouteID              string   `json:"route_id"`
			RouteRequestHash     string   `json:"route_request_hash"`
			RouteAdmissionRow    string   `json:"route_admission_row"`
			RequestedOutcome     string   `json:"requested_outcome"`
			SelectedOutcome      string   `json:"selected_outcome"`
			CorrelationState     string   `json:"correlation_state"`
			CorrelationReason    string   `json:"correlation_reason"`
			Reason               string   `json:"reason"`
			Execution            string   `json:"execution"`
			ReplayBoundary       string   `json:"replay_boundary"`
			RetainedEffects      []string `json:"retained_effects"`
			NonClaims            []string `json:"non_claims"`
			InvokesHostRoute     bool     `json:"invokes_host_route"`
			AllowsHostRoute      bool     `json:"allows_host_route_invocation"`
			BlocksOrdinaryApply  bool     `json:"blocks_ordinary_apply"`
		} `json:"relation_actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan json: %v", err)
	}
	assertNoLegacyRelationActionJSONField(t, stdout.String())
	if !payload.HasErrors {
		t.Fatalf("HasErrors = false, want blocked carrier action to mark errors")
	}
	if len(payload.Actions) != 0 || len(payload.DelegateActions) != 0 {
		t.Fatalf("actions/delegate_actions = %#v/%#v, want no file or delegate action", payload.Actions, payload.DelegateActions)
	}
	if len(payload.RelationActions) != 1 {
		t.Fatalf("relation_actions = %#v, want one", payload.RelationActions)
	}
	got := payload.RelationActions[0]
	if got.Kind != "create" ||
		got.Subject == nil ||
		got.Subject.Kind != "host_relation" ||
		got.Subject.Namespace != "claude-code.plugin-carrier" ||
		got.Subject.Name != "context7" ||
		got.Target != string(target.TargetClaudeCode) ||
		got.Scope != string(target.ScopeProject) ||
		got.RelationSubjectKey != "context7@market" ||
		got.EvidenceSource != "passive_relation_inventory" ||
		got.EvidenceAvailability != "supported" ||
		got.EvidenceFreshness != "fresh" ||
		got.RouteID == "" ||
		!strings.HasPrefix(got.RouteRequestHash, "sha256:") ||
		got.RouteAdmissionRow != "RA-01" ||
		got.RequestedOutcome != "ordinary-mutation" ||
		got.SelectedOutcome != "blocked" ||
		got.CorrelationState != "missing" ||
		got.CorrelationReason != "managed_relation_missing" ||
		got.Reason != "route_not_admitted" ||
		got.Execution != "blocked" ||
		got.ReplayBoundary != "locked_route_request_identity_only" ||
		got.InvokesHostRoute ||
		got.AllowsHostRoute ||
		!got.BlocksOrdinaryApply {
		t.Fatalf("relation action = %#v", got)
	}
	assertStringSliceEqual(t, got.RetainedEffects, relationRetainedEffects())
	assertStringSliceEqual(t, got.NonClaims, relationNonClaims())
}

func TestPrintPlanJSONDisclosesHostDelegatedRelationActionWithoutInvocationNonClaim(t *testing.T) {
	action := claudePluginCarrierActionWithSelectedOutcome(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}, reconciliation.AdmissionOutcomeHostDelegated)

	var stdout bytes.Buffer
	err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command: "apply",
		Mode:    "dry-run",
		Reconciliation: reconciliationWithFamilies(
			t, reconciliation.ContextDryRun, reconciliation.Result{},
			[]reconciliation.RelationAction{action}, nil,
		),
	})
	if err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	var payload struct {
		HasErrors       bool `json:"has_errors"`
		RelationActions []struct {
			Kind                string   `json:"kind"`
			SelectedOutcome     string   `json:"selected_outcome"`
			Execution           string   `json:"execution"`
			Reason              string   `json:"reason"`
			CorrelationState    string   `json:"correlation_state"`
			ReplayBoundary      string   `json:"replay_boundary"`
			NonClaims           []string `json:"non_claims"`
			InvokesHostRoute    bool     `json:"invokes_host_route"`
			AllowsHostRoute     bool     `json:"allows_host_route_invocation"`
			BlocksOrdinaryApply bool     `json:"blocks_ordinary_apply"`
		} `json:"relation_actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan json: %v", err)
	}
	if payload.HasErrors {
		t.Fatalf("HasErrors = true, want host-delegated pending action not to fail by itself")
	}
	if len(payload.RelationActions) != 1 {
		t.Fatalf("relation_actions = %#v, want one", payload.RelationActions)
	}
	got := payload.RelationActions[0]
	if got.Kind != "create" ||
		got.SelectedOutcome != "host-delegated" ||
		got.Execution != "host_route" ||
		got.Reason != "" ||
		got.CorrelationState != "missing" ||
		got.ReplayBoundary != "locked_route_request_identity_only" ||
		!got.InvokesHostRoute ||
		!got.AllowsHostRoute ||
		got.BlocksOrdinaryApply {
		t.Fatalf("host-delegated relation action = %#v", got)
	}
	if slices.Contains(got.NonClaims, "host_route_invocation") {
		t.Fatalf("non_claims = %#v, want no host_route_invocation non-claim for invoking action", got.NonClaims)
	}
	for _, required := range []string{
		"exact_artifact_replay",
		"runtime_readiness",
		"package_cache_convergence",
		"future_skip_authority",
	} {
		if !slices.Contains(got.NonClaims, required) {
			t.Fatalf("non_claims = %#v, want retained non-claim %q", got.NonClaims, required)
		}
	}
}

func TestPrintPlanJSONIncludesNoOpRelationAction(t *testing.T) {
	record, subject := claudePluginCarrierFixture(t)
	action := claudePluginCarrierActionForSubject(t, record, subject, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			claudePluginManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey())),
		},
	})

	var stdout bytes.Buffer
	err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command: "status",
		Mode:    "status",
		Reconciliation: reconciliationWithFamilies(
			t, reconciliation.ContextInspect, reconciliation.Result{},
			[]reconciliation.RelationAction{action}, nil,
		),
	})
	if err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	var payload struct {
		HasErrors       bool `json:"has_errors"`
		RelationActions []struct {
			Kind                 string   `json:"kind"`
			RouteAdmissionRow    string   `json:"route_admission_row"`
			RequestedOutcome     string   `json:"requested_outcome"`
			SelectedOutcome      string   `json:"selected_outcome"`
			EvidenceAvailability string   `json:"evidence_availability"`
			EvidenceFreshness    string   `json:"evidence_freshness"`
			ReplayBoundary       string   `json:"replay_boundary"`
			RetainedEffects      []string `json:"retained_effects"`
			NonClaims            []string `json:"non_claims"`
			CorrelationState     string   `json:"correlation_state"`
			Reason               string   `json:"reason"`
			Execution            string   `json:"execution"`
			BlocksOrdinaryApply  bool     `json:"blocks_ordinary_apply"`
			InvokesHostRoute     bool     `json:"invokes_host_route"`
			AllowsHostRoute      bool     `json:"allows_host_route_invocation"`
		} `json:"relation_actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan json: %v", err)
	}
	assertNoLegacyRelationActionJSONField(t, stdout.String())
	if payload.HasErrors {
		t.Fatalf("HasErrors = true, want no-op carrier action not to fail by itself")
	}
	if len(payload.RelationActions) != 1 {
		t.Fatalf("relation_actions = %#v, want one", payload.RelationActions)
	}
	got := payload.RelationActions[0]
	if got.Kind != "no_op" ||
		got.RouteAdmissionRow != "RA-01" ||
		got.RequestedOutcome != "ordinary-mutation" ||
		got.SelectedOutcome != "blocked" ||
		got.EvidenceAvailability != "supported" ||
		got.EvidenceFreshness != "fresh" ||
		got.ReplayBoundary != "locked_route_request_identity_only" ||
		got.CorrelationState != "exact_correlation" ||
		got.Reason != "" ||
		got.Execution != "no_mutation" ||
		got.BlocksOrdinaryApply ||
		got.InvokesHostRoute ||
		got.AllowsHostRoute {
		t.Fatalf("no-op carrier action = %#v", got)
	}
	assertStringSliceEqual(t, got.RetainedEffects, relationRetainedEffects())
	assertStringSliceEqual(t, got.NonClaims, relationNonClaims())
}

func TestPrintPlanJSONDisclosesStaleRelationEvidence(t *testing.T) {
	record, subject := claudePluginCarrierFixture(t)
	action := claudePluginCarrierActionForSubject(t, record, subject, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceStale,
		Rows: []observeclaudeplugin.Row{
			claudePluginManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey())),
		},
	})

	var stdout bytes.Buffer
	err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command: "status",
		Mode:    "status",
		Reconciliation: reconciliationWithFamilies(
			t, reconciliation.ContextInspect, reconciliation.Result{},
			[]reconciliation.RelationAction{action}, nil,
		),
	})
	if err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	var payload struct {
		HasErrors       bool `json:"has_errors"`
		RelationActions []struct {
			Kind                 string   `json:"kind"`
			CorrelationState     string   `json:"correlation_state"`
			EvidenceAvailability string   `json:"evidence_availability"`
			EvidenceFreshness    string   `json:"evidence_freshness"`
			Reason               string   `json:"reason"`
			Execution            string   `json:"execution"`
			Watchpoints          []string `json:"watchpoints"`
			BlocksOrdinaryApply  bool     `json:"blocks_ordinary_apply"`
			InvokesHostRoute     bool     `json:"invokes_host_route"`
			AllowsHostRoute      bool     `json:"allows_host_route_invocation"`
		} `json:"relation_actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan json: %v", err)
	}
	if !payload.HasErrors {
		t.Fatalf("HasErrors = false, want stale relation evidence to block ordinary status")
	}
	if len(payload.RelationActions) != 1 {
		t.Fatalf("relation_actions = %#v, want one", payload.RelationActions)
	}
	got := payload.RelationActions[0]
	if got.Kind != "block" ||
		got.CorrelationState != "stale_evidence" ||
		got.EvidenceAvailability != "supported" ||
		got.EvidenceFreshness != "stale" ||
		got.Reason != "stale_evidence" ||
		got.Execution != "blocked" ||
		strings.Join(got.Watchpoints, ",") != "fresh_inventory_required" ||
		!got.BlocksOrdinaryApply ||
		got.InvokesHostRoute ||
		got.AllowsHostRoute {
		t.Fatalf("stale relation action = %#v", got)
	}
}

func TestPrintApplyResultJSONIncludesObserveOnlyRelationActions(t *testing.T) {
	action := claudePluginCarrierAction(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnsupported,
		Freshness:    observerelation.EvidenceFresh,
	})

	var stdout bytes.Buffer
	err := PrintApplyResultJSON(&stdout, ApplyResultJSONInput{
		StatefilePath: "/repo/.daem/state.json",
		Reconciliation: reconciliationWithFamilies(
			t, reconciliation.ContextApply, reconciliation.Result{},
			[]reconciliation.RelationAction{action}, nil,
		),
	})
	if err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}

	var payload struct {
		HasErrors       bool `json:"has_errors"`
		RelationActions []struct {
			Kind                 string   `json:"kind"`
			RouteAdmissionRow    string   `json:"route_admission_row"`
			RequestedOutcome     string   `json:"requested_outcome"`
			SelectedOutcome      string   `json:"selected_outcome"`
			Execution            string   `json:"execution"`
			Reason               string   `json:"reason"`
			Watchpoints          []string `json:"watchpoints"`
			EvidenceAvailability string   `json:"evidence_availability"`
			EvidenceFreshness    string   `json:"evidence_freshness"`
			ReplayBoundary       string   `json:"replay_boundary"`
			RetainedEffects      []string `json:"retained_effects"`
			NonClaims            []string `json:"non_claims"`
			BlocksOrdinaryApply  bool     `json:"blocks_ordinary_apply"`
			InvokesHostRoute     bool     `json:"invokes_host_route"`
			AllowsHostRoute      bool     `json:"allows_host_route_invocation"`
		} `json:"relation_actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode apply result json: %v", err)
	}
	assertNoLegacyRelationActionJSONField(t, stdout.String())
	if payload.HasErrors {
		t.Fatalf("HasErrors = true, want observe-only row not to fail by itself")
	}
	if len(payload.RelationActions) != 1 {
		t.Fatalf("relation_actions = %#v, want one", payload.RelationActions)
	}
	got := payload.RelationActions[0]
	if got.Kind != "observe_only" ||
		got.RouteAdmissionRow != "RA-01" ||
		got.RequestedOutcome != "ordinary-mutation" ||
		got.SelectedOutcome != "blocked" ||
		got.Execution != "observe_only" ||
		got.Reason != "unsupported_passive_inventory" ||
		strings.Join(got.Watchpoints, ",") != "passive_inventory_required" ||
		got.EvidenceAvailability != "unsupported" ||
		got.EvidenceFreshness != "fresh" ||
		got.ReplayBoundary != "locked_route_request_identity_only" ||
		got.BlocksOrdinaryApply ||
		got.InvokesHostRoute ||
		got.AllowsHostRoute {
		t.Fatalf("observe-only action = %#v", got)
	}
	assertStringSliceEqual(t, got.RetainedEffects, relationRetainedEffects())
	assertStringSliceEqual(t, got.NonClaims, relationNonClaims())
}

func TestPrintPlanJSONRelationActionsDoNotExposeOwnershipScalars(t *testing.T) {
	action := claudePluginCarrierAction(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})

	var stdout bytes.Buffer
	err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command: "status",
		Mode:    "status",
		Reconciliation: reconciliationWithFamilies(
			t, reconciliation.ContextInspect, reconciliation.Result{},
			[]reconciliation.RelationAction{action}, nil,
		),
	})
	if err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	for _, forbiddenField := range []string{
		`"installed"`,
		`"enabled"`,
		`"ready"`,
		`"loaded"`,
		`"converged"`,
		`"managed"`,
	} {
		if strings.Contains(stdout.String(), forbiddenField) {
			t.Fatalf("json = %s, want no ownership/readiness scalar field %s", stdout.String(), forbiddenField)
		}
	}
}

func assertNoLegacyRelationActionJSONField(t *testing.T, payload string) {
	t.Helper()
	if strings.Contains(payload, `"claude_plugin_carrier_actions"`) {
		t.Fatalf("json = %s, want no legacy claude_plugin_carrier_actions field", payload)
	}
}

func assertStringSliceEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("slice = %#v, want %#v", got, want)
	}
}

func TestPrintApplyResultJSONIncludesSubjectProjectionActions(t *testing.T) {
	var stdout bytes.Buffer
	err := PrintApplyResultJSON(&stdout, ApplyResultJSONInput{
		ActionCount:    1,
		StatefilePath:  "/repo/.daem/state.json",
		Reconciliation: mcpProjectionPlan(t),
	})
	if err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}

	var payload applyResultJSONTestPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode apply json: %v", err)
	}
	if len(payload.Actions) != 1 || payload.Actions[0].Subject == nil {
		t.Fatalf("actions = %#v, want subject action", payload.Actions)
	}
}
