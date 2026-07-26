package reconcile_test

import (
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	reconcile "github.com/isty2e/daem/internal/reconcile"
)

func TestResultRejectsInvalidCorrelationAdmissionAndRouteFacts(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	input := validInput(t, subject, correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}), blockedAdmission(t))

	unknownState := input
	unknownState.Correlation = observerelation.CorrelationResult{}
	_, err := reconcile.NewRelationAction(unknownState)
	if err == nil || !strings.Contains(err.Error(), "correlation state") {
		t.Fatalf("Plan returned error %v, want unsupported correlation state error", err)
	}

	missingAdmission := input
	missingAdmission.RouteAdmission = reconcile.RelationRouteAdmissionDecision{}
	_, err = reconcile.NewRelationAction(missingAdmission)
	if err == nil || !strings.Contains(err.Error(), "route admission row") {
		t.Fatalf("Plan returned error %v, want route admission row error", err)
	}

	invalidRouteRequest := input
	invalidRouteRequest.RouteRequest = realizationdelegate.Request{}
	_, err = reconcile.NewRelationAction(invalidRouteRequest)
	if err == nil || !strings.Contains(err.Error(), "route id") {
		t.Fatalf("Plan returned error %v, want invalid route request error", err)
	}
}

func TestNewRouteAdmissionDecisionRejectsInvalidRowIdentity(t *testing.T) {
	tests := []struct {
		name string
		row  reconcile.RelationRouteAdmissionRow
		want string
	}{
		{name: "empty", row: "", want: "route admission row"},
		{name: "whitespace", row: "  ", want: "route admission row"},
		{name: "untrimmed", row: " RA-01 ", want: "trimmed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reconcile.NewRelationRouteAdmissionDecision(reconcile.RelationRouteAdmissionSpec{
				Row:               tt.row,
				RequestedOutcome:  reconcile.AdmissionOutcomeOrdinaryMutation,
				SelectedOutcome:   reconcile.AdmissionOutcomeBlocked,
				ObservationPolicy: reconcile.ObservationRequireCurrent,
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewRouteAdmissionDecision returned error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNewRouteAdmissionDecisionRejectsUnsupportedObservationPolicyCombinations(t *testing.T) {
	tests := []struct {
		name     string
		selected reconcile.RelationAdmissionOutcome
		policy   reconcile.RelationObservationPolicy
		want     string
	}{
		{name: "zero policy", selected: reconcile.AdmissionOutcomeHostDelegated, want: "observation policy"},
		{name: "unknown policy", selected: reconcile.AdmissionOutcomeHostDelegated, policy: "sometimes", want: "unsupported"},
		{name: "attempt without host route", selected: reconcile.AdmissionOutcomeBlocked, policy: reconcile.ObservationAttemptWhenUnsupported, want: "requires a host-route"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reconcile.NewRelationRouteAdmissionDecision(reconcile.RelationRouteAdmissionSpec{
				Row:               routeAdmissionRowInstallCarrier,
				RequestedOutcome:  reconcile.AdmissionOutcomeOrdinaryMutation,
				SelectedOutcome:   tt.selected,
				ObservationPolicy: tt.policy,
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewRouteAdmissionDecision returned error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestResultRejectsInvalidCanonicalInputAxes(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	input := validInput(t, subject, correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}), blockedAdmission(t))

	tests := []struct {
		name   string
		mutate func(*reconcile.RelationActionInput)
		want   string
	}{
		{
			name: "zero carrier identity",
			mutate: func(input *reconcile.RelationActionInput) {
				input.CarrierIdentity = durablecarrier.ManagedCarrierIdentity{}
			},
			want: "carrier identity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := input
			tt.mutate(&candidate)
			_, err := reconcile.NewRelationAction(candidate)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Plan returned error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestResultRejectsExactCorrelationFromDifferentSubject(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	other := mustSubject(t, "other-plugin", "managed/other-plugin")
	otherCorrelation := correlationFor(t, other, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observerelation.Row{
			mustManagedRow(t, "other-plugin", "managed/other-plugin"),
		},
	})

	_, err := reconcile.NewRelationAction(validInput(t, subject, otherCorrelation, blockedAdmission(t)))
	if err == nil || !strings.Contains(err.Error(), "relation subject key") {
		t.Fatalf("Plan returned error %v, want relation subject key mismatch", err)
	}

	otherManagedKey := mustSubject(t, "context7", "managed/other")
	otherManagedKeyCorrelation := correlationFor(t, otherManagedKey, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observerelation.Row{
			mustManagedRow(t, "context7", "managed/other"),
		},
	})

	_, err = reconcile.NewRelationAction(validInput(t, subject, otherManagedKeyCorrelation, blockedAdmission(t)))
	if err == nil || !strings.Contains(err.Error(), "managed instance key") {
		t.Fatalf("Plan returned error %v, want managed instance key mismatch", err)
	}
}
