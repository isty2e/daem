package hostroute

import (
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
)

func TestClassifyResultAbsentPostconditionRequiresFreshMissingRelation(t *testing.T) {
	fixture, command := resultFixture(t)
	result, err := ClassifyResult(ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      successfulAttempt(t),
		Observation: observationFact(t, fixture, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
		}),
		RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionAbsent),
	})
	if err != nil {
		t.Fatalf("ClassifyResult returned error: %v", err)
	}
	if result.Class() != ResultAttemptedObservedAbsent ||
		result.StateSummary().Reason() != ResultReasonObservedAbsent {
		t.Fatalf("result class/reason = %q/%q", result.Class(), result.StateSummary().Reason())
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationMissing,
		observerelation.PostconditionObserved,
	)
}

func TestClassifyResultAbsentPostconditionRejectsStillPresentRelation(t *testing.T) {
	fixture, command := resultFixture(t)
	result, err := ClassifyResult(ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      successfulAttempt(t),
		Observation: observationFact(t, fixture, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				mustClaudeRow(
					t,
					"context7@market",
					string(fixture.subject.ExpectedRelation().ManagedInstanceKey()),
					true,
				),
			},
		}),
		RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionAbsent),
	})
	if err != nil {
		t.Fatalf("ClassifyResult returned error: %v", err)
	}
	if result.Class() != ResultAttemptedObservedPresent ||
		result.StateSummary().Reason() != ResultReasonObservedPresent {
		t.Fatalf("result class/reason = %q/%q", result.Class(), result.StateSummary().Reason())
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationPresent,
		observerelation.PostconditionFailed,
	)
}

func TestClassifyResultAbsentPostconditionDoesNotAcceptExternalSameSubject(t *testing.T) {
	fixture, command := resultFixture(t)
	result, err := ClassifyResult(ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      successfulAttempt(t),
		Observation: observationFact(t, fixture, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				mustClaudeRow(t, "context7@market", "", false),
			},
		}),
		RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionAbsent),
	})
	if err != nil {
		t.Fatalf("ClassifyResult returned error: %v", err)
	}
	if result.Class() != ResultBlocked ||
		result.StateSummary().Reason() != ResultReasonUnkeyedSameSubject {
		t.Fatalf("result class/reason = %q/%q", result.Class(), result.StateSummary().Reason())
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationUnknown,
		observerelation.PostconditionUnknown,
	)
}

func TestClassifyResultAbsentPostconditionRejectsStaleMissingEvidence(t *testing.T) {
	fixture, command := resultFixture(t)
	result, err := ClassifyResult(ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      successfulAttempt(t),
		Observation: observationFact(t, fixture, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceStale,
		}),
		RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionAbsent),
	})
	if err != nil {
		t.Fatalf("ClassifyResult returned error: %v", err)
	}
	if result.Class() != ResultAttemptedUnverified ||
		result.StateSummary().Reason() != ResultReasonObservationStale {
		t.Fatalf("result class/reason = %q/%q", result.Class(), result.StateSummary().Reason())
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationUnknown,
		observerelation.PostconditionUnknown,
	)
}

func TestClassifyResultAbsentPostconditionDoesNotTurnFailedAttemptIntoSuccess(t *testing.T) {
	fixture, command := resultFixture(t)
	result, err := ClassifyResult(ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      failedAttempt(t),
		Observation: observationFact(t, fixture, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
		}),
		RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionAbsent),
	})
	if err != nil {
		t.Fatalf("ClassifyResult returned error: %v", err)
	}
	if result.Class() != ResultFailed ||
		result.StateSummary().Reason() != ResultReasonNonZeroExit {
		t.Fatalf("result class/reason = %q/%q", result.Class(), result.StateSummary().Reason())
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationMissing,
		observerelation.PostconditionObserved,
	)
}
