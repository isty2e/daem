package clipresent

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
)

func TestPlanJSONDisclosesExactStateOnlyCarrierAdoptionWithoutAuthorityOverclaim(t *testing.T) {
	fixture := newPresentCarrierAdoptionFixture(t)
	action := fixture.action(t, true, fixture.lifecycle, nil)
	plan, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          reconcile.ContextApply,
		CarrierAdoptions: []carrieradoption.Action{action},
	})
	if err != nil {
		t.Fatalf("reconcile.NewResult: %v", err)
	}

	var output bytes.Buffer
	if err := PrintPlanJSON(&output, PlanJSONInput{
		Command:        "apply",
		Mode:           "dry-run",
		Reconciliation: plan,
	}); err != nil {
		t.Fatalf("PrintPlanJSON: %v", err)
	}
	var payload struct {
		SchemaVersion    int                         `json:"schema_version"`
		HasErrors        bool                        `json:"has_errors"`
		CarrierAdoptions []carrierAdoptionActionJSON `json:"carrier_adoption_actions"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if payload.SchemaVersion != 10 || payload.HasErrors || len(payload.CarrierAdoptions) != 1 {
		t.Fatalf("payload = %#v, want schema 10 with one nonblocking adoption", payload)
	}
	row := payload.CarrierAdoptions[0]
	if row.Result != "eligible_exact_relation" ||
		row.Target != "claude-code" ||
		row.Scope != "project" ||
		row.SourceNamespace != "marketplace:context7@market" ||
		row.ClaimOwner != "selected_manifest" ||
		row.ProposedClaimProvenance != "explicitly_adopted_observed" ||
		row.ClaimTransition != "would_record" ||
		!row.LifecycleEligible ||
		row.LifecycleBlocker != "" ||
		row.AmbientConsumerAssurance != "not_proven" ||
		row.InvokesHostRoute ||
		!row.StateOnly {
		t.Fatalf("carrier adoption row = %#v", row)
	}
	if row.InstallRouteID == "" ||
		row.InstallRouteRequestHash == "" ||
		row.RemovalRouteID == "" ||
		row.RemovalRouteRequestHash == "" ||
		row.LaterOmission != "requests_managed_relation_absence" ||
		len(row.NonClaims) == 0 {
		t.Fatalf("carrier adoption lifecycle disclosure = %#v", row)
	}
}

func TestCarrierAdoptionHumanHintRequiresLifecycleEligibility(t *testing.T) {
	fixture := newPresentCarrierAdoptionFixture(t)
	eligible := fixture.action(t, false, fixture.lifecycle, nil)
	ineligible := fixture.action(
		t,
		false,
		fixture.lifecycleWithStoreAvailability(t, false),
		nil,
	)

	var eligibleOutput bytes.Buffer
	PrintCarrierAdoptionActionsWithOptions(
		&eligibleOutput,
		[]carrieradoption.Action{eligible},
		HumanOptions{},
	)
	if !strings.Contains(eligibleOutput.String(), "carrier adoption available") ||
		!strings.Contains(eligibleOutput.String(), "daem apply --manage-existing --dry-run") {
		t.Fatalf("eligible output = %q", eligibleOutput.String())
	}

	var ineligibleOutput bytes.Buffer
	PrintCarrierAdoptionActionsWithOptions(
		&ineligibleOutput,
		[]carrieradoption.Action{ineligible},
		HumanOptions{},
	)
	if !strings.Contains(ineligibleOutput.String(), "claim store unavailable") {
		t.Fatalf("ineligible output = %q", ineligibleOutput.String())
	}
	if strings.Contains(ineligibleOutput.String(), "--manage-existing") {
		t.Fatalf("ineligible output offered adoption hint: %q", ineligibleOutput.String())
	}
}

func TestPresentUnclaimedRelationSummaryReportsFactInsteadOfGenericBlock(t *testing.T) {
	record, relation := claudePluginCarrierFixture(t)
	expected := relation.ExpectedRelation()
	action := claudePluginCarrierActionForSubjectWithClaimPresence(
		t,
		record,
		relation,
		observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				claudePluginManagedRow(
					t,
					string(expected.SubjectKey()),
					string(expected.ManagedInstanceKey()),
				),
			},
		},
		reconcile.AdmissionOutcomeBlocked,
		false,
	)
	if action.Reason() != reconcile.ReasonPresentUnclaimed {
		t.Fatalf("relation reason = %s, want %s", action.Reason(), reconcile.ReasonPresentUnclaimed)
	}

	var output bytes.Buffer
	PrintRelationActionsWithOptions(
		&output,
		[]reconcile.RelationAction{action},
		HumanOptions{},
	)
	if !strings.Contains(output.String(), "external carrier present but unclaimed") {
		t.Fatalf("relation output = %q", output.String())
	}
	if strings.Contains(output.String(), "  - blocked ") {
		t.Fatalf("relation output contradicted the adoption plan: %q", output.String())
	}
}

func TestCarrierAdoptionHumanOutputDistinguishesEveryNonEligibleOutcome(t *testing.T) {
	fixture := newPresentCarrierAdoptionFixture(t)
	identity := presentManagedCarrierIdentity(t, fixture.contract)
	expected := identity.ExpectedRelation()
	unkeyedRow, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey: string(expected.SubjectKey()),
	})
	if err != nil {
		t.Fatalf("NewRow: %v", err)
	}
	correlation := func(spec observerelation.InventorySpec) observerelation.CorrelationResult {
		t.Helper()
		inventory, inventoryErr := observerelation.NewInventory(spec)
		if inventoryErr != nil {
			t.Fatalf("NewInventory: %v", inventoryErr)
		}
		return observerelation.Correlate(expected, inventory)
	}
	missing := correlation(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	inexact := correlation(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observerelation.Row{unkeyedRow},
	})
	blocked := correlation(observerelation.InventorySpec{
		Availability: observerelation.InventoryUnavailable,
		Freshness:    observerelation.EvidenceFresh,
	})
	currentClaim := fixture.claim(
		t,
		fixture.owner,
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	)
	otherRoot := t.TempDir()
	otherOwner, err := stateauthority.New(pathtest.Exact(
		filepath.Join(otherRoot, ".daem", "state.json"),
	),

		filepath.Join(otherRoot, "other.toml"))
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	conflictingClaim := fixture.claim(
		t,
		otherOwner,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)

	tests := []struct {
		name       string
		action     carrieradoption.Action
		want       string
		forbidHint bool
	}{
		{
			name:   "already claimed",
			action: fixture.action(t, true, fixture.lifecycle, []durablecarrier.ManagedCarrierClaim{currentClaim}),
			want:   "already claimed",
		},
		{
			name:       "claim conflict",
			action:     fixture.action(t, true, fixture.lifecycle, []durablecarrier.ManagedCarrierClaim{conflictingClaim}),
			want:       "blocked carrier adoption",
			forbidHint: true,
		},
		{
			name:       "missing",
			action:     fixture.actionWithObservation(t, missing, true, fixture.lifecycle, nil),
			want:       "relation missing",
			forbidHint: true,
		},
		{
			name:       "inexact",
			action:     fixture.actionWithObservation(t, inexact, true, fixture.lifecycle, nil),
			want:       "not source-exact",
			forbidHint: true,
		},
		{
			name:       "observation blocked",
			action:     fixture.actionWithObservation(t, blocked, true, fixture.lifecycle, nil),
			want:       "observation",
			forbidHint: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			PrintCarrierAdoptionActionsWithOptions(
				&output,
				[]carrieradoption.Action{tt.action},
				HumanOptions{},
			)
			if !strings.Contains(output.String(), tt.want) {
				t.Fatalf("output = %q, want %q", output.String(), tt.want)
			}
			if tt.forbidHint && strings.Contains(output.String(), "--manage-existing") {
				t.Fatalf("output offered unsafe adoption hint: %q", output.String())
			}
		})
	}
}

func TestApplyResultJSONDistinguishesCommittedAndFailedCarrierAdoption(t *testing.T) {
	fixture := newPresentCarrierAdoptionFixture(t)
	action := fixture.action(t, true, fixture.lifecycle, nil)
	plan, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          reconcile.ContextApply,
		CarrierAdoptions: []carrieradoption.Action{action},
	})
	if err != nil {
		t.Fatalf("reconcile.NewResult: %v", err)
	}
	recordedClaim, present := action.ProposedClaim()
	if !present {
		t.Fatal("eligible carrier adoption has no proposed claim")
	}

	for _, tt := range []struct {
		name               string
		err                error
		executionAttempted bool
		results            []durablecarrier.ManagedCarrierClaim
		wantTransition     string
		wantErrors         bool
	}{
		{
			name:           "committed",
			results:        []durablecarrier.ManagedCarrierClaim{recordedClaim},
			wantTransition: "recorded",
		},
		{name: "failed before execution", err: errors.New("readiness changed"), wantTransition: "not_recorded", wantErrors: true},
		{name: "failed after execution attempt", err: errors.New("host route failed"), executionAttempted: true, wantTransition: "unknown_after_error", wantErrors: true},
		{
			name:               "committed before later failure",
			err:                errors.New("later phase failed"),
			executionAttempted: true,
			results:            []durablecarrier.ManagedCarrierClaim{recordedClaim},
			wantTransition:     "recorded",
			wantErrors:         true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := PrintApplyResultJSON(&output, ApplyResultJSONInput{
				Reconciliation:         plan,
				ExecutionAttempted:     tt.executionAttempted,
				CarrierAdoptionResults: tt.results,
				Err:                    tt.err,
			}); err != nil {
				t.Fatalf("PrintApplyResultJSON: %v", err)
			}
			var payload struct {
				SchemaVersion    int                         `json:"schema_version"`
				HasErrors        bool                        `json:"has_errors"`
				CarrierAdoptions []carrierAdoptionActionJSON `json:"carrier_adoption_actions"`
			}
			if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if payload.SchemaVersion != 15 ||
				payload.HasErrors != tt.wantErrors ||
				len(payload.CarrierAdoptions) != 1 ||
				payload.CarrierAdoptions[0].ClaimTransition != tt.wantTransition {
				t.Fatalf("payload = %#v", payload)
			}
		})
	}
}

func TestApplyResultDistinguishesPendingInstallRecoveryFromExplicitAdoption(t *testing.T) {
	fixture := newPresentCarrierAdoptionFixture(t)
	action := fixture.action(t, true, fixture.lifecycle, nil)
	plan, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          reconcile.ContextApply,
		CarrierAdoptions: []carrieradoption.Action{action},
	})
	if err != nil {
		t.Fatalf("reconcile.NewResult: %v", err)
	}
	recovered := fixture.claim(
		t,
		fixture.owner,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)

	var jsonOutput bytes.Buffer
	if err := PrintApplyResultJSON(&jsonOutput, ApplyResultJSONInput{
		Reconciliation:         plan,
		ExecutionAttempted:     true,
		CarrierAdoptionResults: []durablecarrier.ManagedCarrierClaim{recovered},
		Err:                    errors.New("later phase failed"),
	}); err != nil {
		t.Fatalf("PrintApplyResultJSON: %v", err)
	}
	var payload struct {
		SchemaVersion    int                         `json:"schema_version"`
		CarrierAdoptions []carrierAdoptionActionJSON `json:"carrier_adoption_actions"`
	}
	if err := json.Unmarshal(jsonOutput.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if payload.SchemaVersion != 15 || len(payload.CarrierAdoptions) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	row := payload.CarrierAdoptions[0]
	if row.ClaimTransition != "completed_by_install_recovery" ||
		row.ProposedClaimProvenance != "explicitly_adopted_observed" ||
		row.FinalClaimProvenance != "installed_observed_transition" {
		t.Fatalf("carrier adoption row = %#v", row)
	}

	var humanOutput bytes.Buffer
	PrintCarrierAdoptionResultsWithOptions(
		&humanOutput,
		[]carrieradoption.Action{action},
		[]durablecarrier.ManagedCarrierClaim{recovered},
		false,
		HumanOptions{},
	)
	if !strings.Contains(humanOutput.String(), "completed pending carrier install recovery") ||
		!strings.Contains(humanOutput.String(), "provenance=installed_observed_transition") {
		t.Fatalf("human output = %q", humanOutput.String())
	}
	if strings.Contains(humanOutput.String(), "recorded external carrier claim") {
		t.Fatalf("human output mislabeled install recovery: %q", humanOutput.String())
	}
}

func TestCarrierAdoptionVerboseOutputMatchesJSONWithoutAuthorityPaths(t *testing.T) {
	fixture := newPresentCarrierAdoptionFixture(t)
	action := fixture.action(t, true, fixture.lifecycle, nil)

	var output bytes.Buffer
	PrintCarrierAdoptionActionsWithOptions(
		&output,
		[]carrieradoption.Action{action},
		HumanOptions{Verbose: true},
	)
	text := output.String()
	for _, expected := range []string{
		"result=eligible_exact_relation",
		`source_namespace="marketplace:context7@market"`,
		"claim_transition=would_record",
		"lifecycle_eligible=true",
		"ambient_consumer_assurance=not_proven",
		"invokes_host_route=false",
		"state_only=true",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("verbose output %q omitted %q", text, expected)
		}
	}
	if strings.Contains(text, fixture.owner.StatefileKey()) ||
		strings.Contains(text, fixture.owner.ManifestPath()) {
		t.Fatalf("verbose output leaked authority paths: %q", text)
	}
}

func TestCarrierAdoptionResultHumanOutputDoesNotCallFailedClaimManaged(t *testing.T) {
	fixture := newPresentCarrierAdoptionFixture(t)
	action := fixture.action(t, true, fixture.lifecycle, nil)

	var output bytes.Buffer
	PrintCarrierAdoptionResultsWithOptions(
		&output,
		[]carrieradoption.Action{action},
		nil,
		false,
		HumanOptions{},
	)
	if !strings.Contains(output.String(), "claim outcome unconfirmed after apply error") ||
		!strings.Contains(output.String(), "next: daem status") {
		t.Fatalf("failed result output = %q", output.String())
	}
	if strings.Contains(output.String(), "already claimed") ||
		strings.Contains(output.String(), "recorded external carrier claim") {
		t.Fatalf("failed result overclaimed authority: %q", output.String())
	}

	recorded, present := action.ProposedClaim()
	if !present {
		t.Fatal("eligible carrier adoption has no proposed claim")
	}
	output.Reset()
	PrintCarrierAdoptionResultsWithOptions(
		&output,
		[]carrieradoption.Action{action},
		[]durablecarrier.ManagedCarrierClaim{recorded},
		false,
		HumanOptions{},
	)
	if !strings.Contains(output.String(), "recorded external carrier claim") ||
		strings.Contains(output.String(), "claim outcome unconfirmed") {
		t.Fatalf("committed partial result output = %q", output.String())
	}
}
