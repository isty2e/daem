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
	"github.com/isty2e/daem/internal/contractversion"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
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
	if payload.SchemaVersion != contractversion.ReconciliationPlanJSON || payload.HasErrors || len(payload.CarrierAdoptions) != 1 {
		t.Fatalf("payload = %#v, want current plan schema with one nonblocking adoption", payload)
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

func TestCarrierAdoptionRedactsLocalSourceOutsideVerboseHumanOutput(t *testing.T) {
	const localSource = "packages/tools"
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "local-pi-extension",
		Carrier: desiredextension.CarrierPiPackage,
		Target:  target.TargetPi,
		Scope:   target.ScopeGlobal,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindHostSource,
			localSource,
		),
	})
	file, relation := snapshottest.ExtensionCarrierFile(t, value)
	fixture := newPresentCarrierAdoptionFixtureFromContract(
		t,
		file.Locked.Subjects()[0],
		relation,
	)
	action := fixture.action(t, true, fixture.lifecycle, nil)
	reconciliation, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          reconcile.ContextApply,
		CarrierAdoptions: []carrieradoption.Action{action},
	})
	if err != nil {
		t.Fatal(err)
	}
	var planJSON bytes.Buffer
	if err := PrintPlanJSON(&planJSON, PlanJSONInput{
		Command:        "apply",
		Mode:           "dry-run",
		Reconciliation: reconciliation,
	}); err != nil {
		t.Fatal(err)
	}
	var applyJSON bytes.Buffer
	if err := PrintApplyResultJSON(&applyJSON, ApplyResultJSONInput{
		Reconciliation: reconciliation,
	}); err != nil {
		t.Fatal(err)
	}
	for label, encoded := range map[string][]byte{
		"plan":  planJSON.Bytes(),
		"apply": applyJSON.Bytes(),
	} {
		if bytes.Contains(encoded, []byte(localSource)) ||
			!bytes.Contains(encoded, []byte(`"name_redacted": true`)) {
			t.Fatalf("%s JSON did not fully redact local carrier identity: %s", label, encoded)
		}
	}

	rows := carrierAdoptionJSONActions(
		[]carrieradoption.Action{action},
		carrierAdoptionPlanned,
	)
	if len(rows) != 1 || !rows[0].SourceNamespaceRedacted ||
		!rows[0].RelationSubjectKeyRedacted ||
		rows[0].CarrierSubject == nil ||
		!rows[0].CarrierSubject.NameRedacted ||
		!strings.HasPrefix(rows[0].SourceNamespace, "redacted:sha256:") ||
		!strings.HasPrefix(rows[0].RelationSubjectKey, "redacted:sha256:") ||
		!strings.HasPrefix(rows[0].CarrierSubject.Name, "redacted:sha256:") ||
		strings.Contains(rows[0].SourceNamespace, localSource) ||
		strings.Contains(rows[0].CarrierSubject.Name, localSource) {
		t.Fatalf("carrier adoption row = %#v", rows)
	}

	var summary bytes.Buffer
	PrintCarrierAdoptionActionsWithOptions(
		&summary,
		[]carrieradoption.Action{action},
		HumanOptions{},
	)
	if strings.Contains(summary.String(), localSource) ||
		!strings.Contains(summary.String(), "redacted:sha256:") {
		t.Fatalf("summary = %q", summary.String())
	}

	var verbose bytes.Buffer
	PrintCarrierAdoptionActionsWithOptions(
		&verbose,
		[]carrieradoption.Action{action},
		HumanOptions{Verbose: true},
	)
	if !strings.Contains(verbose.String(), localSource) {
		t.Fatalf("verbose output = %q, want exact local source", verbose.String())
	}
}

func TestCarrierAdoptionRedactsOpaqueOpenCodeHostSources(t *testing.T) {
	for _, source := range []string{"plugins/local.ts", `plugins\local.ts`} {
		t.Run(source, func(t *testing.T) {
			contract, relation := hostSourceCarrierFixture(
				t,
				"local-opencode",
				desiredextension.CarrierOpenCodePlugin,
				target.TargetOpenCode,
				target.ScopeProject,
				source,
			)
			fixture := newPresentCarrierAdoptionFixtureFromContract(t, contract, relation)
			action := fixture.action(t, true, fixture.lifecycle, nil)
			row := carrierAdoptionJSONActions(
				[]carrieradoption.Action{action},
				carrierAdoptionPlanned,
			)[0]
			if !row.SourceNamespaceRedacted ||
				!row.RelationSubjectKeyRedacted ||
				row.CarrierSubject == nil ||
				!row.CarrierSubject.NameRedacted ||
				strings.Contains(row.SourceNamespace, source) ||
				strings.Contains(row.RelationSubjectKey, source) ||
				strings.Contains(row.CarrierSubject.Name, source) {
				t.Fatalf("carrier adoption row = %#v", row)
			}

			var summary bytes.Buffer
			PrintCarrierAdoptionActionsWithOptions(
				&summary,
				[]carrieradoption.Action{action},
				HumanOptions{},
			)
			if strings.Contains(summary.String(), source) ||
				!strings.Contains(summary.String(), "redacted:sha256:") {
				t.Fatalf("summary = %q", summary.String())
			}
		})
	}
}

func TestVerboseIdentityDisclosureRedactsSensitiveSourceDerivatives(t *testing.T) {
	for _, value := range []string{
		"npm:tool@token:actual-secret",
		`plugins\client-secret=actual-secret`,
	} {
		disclosure := verboseIdentityDisclosureFor(value)
		if !disclosure.Redacted() ||
			strings.Contains(disclosure.Value(), "actual-secret") ||
			!strings.HasPrefix(disclosure.Value(), "redacted:sha256:") {
			t.Fatalf("verbose disclosure for %q = %#v", value, disclosure)
		}
	}
}

func TestUnknownLockOrderGrammarFailsClosed(t *testing.T) {
	disclosure := lockHostLoadIdentityDisclosureFor(
		hostrelation.OrderClassID("extension:unknown:project:plugins"),
		"plugins/local.ts",
	)
	if !disclosure.Redacted() || !strings.HasPrefix(disclosure.Value(), "redacted:sha256:") {
		t.Fatalf("unknown order disclosure = %#v", disclosure)
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
			var failure *applyworkflow.Failure
			if tt.err != nil {
				classified := applyworkflow.ClassifyFailure(
					tt.err,
					applyworkflow.CommandResult{ExecutionAttempted: tt.executionAttempted},
				)
				failure = &classified
			}
			var output bytes.Buffer
			if err := PrintApplyResultJSON(&output, ApplyResultJSONInput{
				Reconciliation:         plan,
				ExecutionAttempted:     tt.executionAttempted,
				CarrierAdoptionResults: tt.results,
				Failure:                failure,
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
			if payload.SchemaVersion != contractversion.ApplyResultJSON ||
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
	failure := applyworkflow.ClassifyFailure(
		errors.New("later phase failed"),
		applyworkflow.CommandResult{ExecutionAttempted: true},
	)

	var jsonOutput bytes.Buffer
	if err := PrintApplyResultJSON(&jsonOutput, ApplyResultJSONInput{
		Reconciliation:         plan,
		ExecutionAttempted:     true,
		CarrierAdoptionResults: []durablecarrier.ManagedCarrierClaim{recovered},
		Failure:                &failure,
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
	if payload.SchemaVersion != contractversion.ApplyResultJSON || len(payload.CarrierAdoptions) != 1 {
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
		errors.New("apply failed after recovery completion"),
		true,
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
		errors.New("readiness changed"),
		false,
		HumanOptions{},
	)
	if !strings.Contains(output.String(), "claim not recorded before apply effects") ||
		strings.Contains(output.String(), "claim outcome unconfirmed") {
		t.Fatalf("pre-effect failure output = %q", output.String())
	}

	output.Reset()
	PrintCarrierAdoptionResultsWithOptions(
		&output,
		[]carrieradoption.Action{action},
		nil,
		errors.New("host route failed"),
		true,
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
		errors.New("later apply phase failed"),
		true,
		HumanOptions{},
	)
	if !strings.Contains(output.String(), "recorded external carrier claim") ||
		strings.Contains(output.String(), "claim outcome unconfirmed") {
		t.Fatalf("committed partial result output = %q", output.String())
	}
}
