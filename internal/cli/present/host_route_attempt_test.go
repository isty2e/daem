package clipresent

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestPrintHostRouteAttemptsMarksHistoryOnlyWithoutConvergenceClaims(t *testing.T) {
	attempt := hostRoutePresentationAttempt(t)

	var stdout bytes.Buffer
	if err := PrintHostRouteAttemptsWithOptions(&stdout, []durableattempt.HostRouteAttempt{attempt}, HumanOptions{Verbose: true}); err != nil {
		t.Fatalf("PrintHostRouteAttemptsWithOptions returned error: %v", err)
	}
	rendered := stdout.String()

	for _, want := range []string{
		"history-only diagnostics",
		"evidence=host_route_attempt_diagnostics",
		"authority=history_only",
		"operation=remove",
		"result_class=attempted_unverified",
		"reason=effect_postcondition_unavailable",
		"observation=missing",
		"postcondition=observed",
		"carrier_artifacts_absent=unavailable",
		"grants_apply_skip_authority=false",
		"future_skip_authority",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("host route attempt output = %q, want %q", rendered, want)
		}
	}
	for _, forbidden := range []string{
		"installed",
		"synced",
		"converged",
		"applied",
		"grants_apply_skip_authority=true",
	} {
		if strings.Contains(strings.ToLower(rendered), forbidden) {
			t.Fatalf("host route attempt output = %q, want no %q", rendered, forbidden)
		}
	}
}

func TestPrintApplyResultJSONHostRouteAttemptsAreHistoryOnlyDiagnostics(t *testing.T) {
	attempt := hostRoutePresentationAttempt(t)

	var stdout bytes.Buffer
	err := PrintApplyResultJSON(&stdout, ApplyResultJSONInput{
		StatefilePath:     "/repo/.daem/state.json",
		HostRouteAttempts: []durableattempt.HostRouteAttempt{attempt},
	})
	if err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}

	var payload struct {
		SchemaVersion     int `json:"schema_version"`
		HostRouteAttempts []struct {
			EvidenceKind         string `json:"evidence_kind"`
			Authority            string `json:"authority"`
			ResultClass          string `json:"result_class"`
			Reason               string `json:"reason"`
			Operation            string `json:"operation"`
			AttemptObserved      bool   `json:"attempt_observed"`
			Observation          string `json:"observation"`
			Postcondition        string `json:"postcondition"`
			EffectPostconditions []struct {
				Requirement string `json:"requirement"`
				State       string `json:"state"`
			} `json:"effect_postconditions"`
			GrantsApplySkipAuthority bool     `json:"grants_apply_skip_authority"`
			NonClaims                []string `json:"non_claims"`
		} `json:"host_route_attempts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode apply result json: %v", err)
	}
	if payload.SchemaVersion != 14 {
		t.Fatalf("schema_version = %d, want 14", payload.SchemaVersion)
	}
	if len(payload.HostRouteAttempts) != 1 {
		t.Fatalf("host_route_attempts = %#v, want one row", payload.HostRouteAttempts)
	}
	got := payload.HostRouteAttempts[0]
	if got.EvidenceKind != "host_route_attempt_diagnostics" ||
		got.Authority != "history_only" ||
		got.ResultClass != string(durableattempt.HostRouteResultAttemptedUnverified) ||
		got.Reason != "effect_postcondition_unavailable" ||
		got.Operation != "remove" ||
		!got.AttemptObserved ||
		got.Observation != string(observerelation.ObservationMissing) ||
		got.Postcondition != string(observerelation.PostconditionObserved) ||
		len(got.EffectPostconditions) != 1 ||
		got.EffectPostconditions[0].Requirement != "carrier_artifacts_absent" ||
		got.EffectPostconditions[0].State != "unavailable" ||
		got.GrantsApplySkipAuthority {
		t.Fatalf("host route attempt json = %#v, want history-only attempted-unverified diagnostics", got)
	}
	if !slices.Contains(got.NonClaims, "future_skip_authority") ||
		!slices.Contains(got.NonClaims, "package_cache_convergence") ||
		!slices.Contains(got.NonClaims, "runtime_readiness") {
		t.Fatalf("non_claims = %#v, want retained-effect non-claims", got.NonClaims)
	}
}

func hostRoutePresentationAttempt(t *testing.T) durableattempt.HostRouteAttempt {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		"context7-managed",
	)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := assurancepostcondition.NewSummary(
		effectpostcondition.CarrierArtifactsAbsent,
		assurancepostcondition.SummaryUnavailable,
	)
	if err != nil {
		t.Fatal(err)
	}
	summaries, err := assurancepostcondition.NewSummarySet(
		[]assurancepostcondition.Summary{summary},
	)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:              subject,
		Target:               target.TargetClaudeCode,
		Scope:                target.ScopeProject,
		Operation:            lock.OperationRemove,
		RouteID:              "claude-code.plugin-carrier.remove",
		RouteRequestHash:     "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		ObservedAt:           time.Date(2026, time.July, 7, 12, 0, 0, 0, time.UTC),
		ResultClass:          durableattempt.HostRouteResultAttemptedUnverified,
		Reason:               durableattempt.HostRouteReasonEffectUnavailable,
		AttemptObserved:      true,
		Observation:          observerelation.ObservationMissing,
		Postcondition:        observerelation.PostconditionObserved,
		EffectPostconditions: summaries,
	})
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}
