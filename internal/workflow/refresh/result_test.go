package refresh

import (
	"errors"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestFailureDetailIsDerivedOnlyFromClosedWorkflowFacts(t *testing.T) {
	causes := []error{
		errors.New(`read /Users/alice/private.json: authorization_code=oauth-secret`),
		errors.New("visible\u202ereordered clientSecret=boundary-secret"),
		errors.New(`open \Users\alice\private.json: denied`),
	}
	for _, cause := range causes {
		result, err := refusedResult(
			CommandResult{Mode: ModeDryRun},
			ReasonManifestUnavailable,
			cause,
			"fix the selected manifest and retry",
		)
		if err == nil {
			t.Fatal("refusedResult returned nil error")
		}
		if !errors.Is(err, cause) {
			t.Fatal("refusal error did not retain its internal cause")
		}
		if got, want := result.FailureDetail(), "the selected manifest is unavailable"; got != want {
			t.Fatalf("FailureDetail() = %q, want %q", got, want)
		}
	}
}

func TestFailureDetailCatalogCoversEveryWorkflowReason(t *testing.T) {
	tests := []struct {
		name   string
		result CommandResult
		want   string
	}{
		{name: "invalid selection", result: failedResult(ReasonInvalidSelection), want: "the selected extension relation is invalid"},
		{name: "manifest unavailable", result: failedResult(ReasonManifestUnavailable), want: "the selected manifest is unavailable"},
		{name: "lock unavailable", result: failedResult(ReasonLockUnavailable), want: "the selected lockfile is unavailable"},
		{name: "lock mismatch", result: failedResult(ReasonLockMismatch), want: "the selected lockfile does not match the manifest"},
		{name: "refresh unsupported", result: failedResult(ReasonRefreshUnsupported), want: "the selected relation has no supported refresh route"},
		{name: "relation missing", result: failedResult(ReasonRelationMissing), want: "the selected relation is missing from current host state"},
		{name: "relation ambiguous", result: failedResult(ReasonRelationAmbiguous), want: "the selected relation is ambiguous in current host state"},
		{name: "observation unavailable", result: failedResult(ReasonObservationUnavailable), want: "required current relation evidence is unavailable"},
		{name: "stale plan", result: failedResult(ReasonStalePlan), want: "the authorized refresh plan is stale"},
		{name: "mutation authority", result: failedResult(ReasonMutationAuthority), want: "required mutation authority is unavailable"},
		{name: "command failed", result: failedResult(ReasonCommandFailed), want: "the delegated host command failed"},
		{name: "invalid timeout", result: failedResult(ReasonInvalidTimeout), want: "the refresh timeout is invalid"},
		{name: "post observation failed", result: failedResult(ReasonPostObservationFailed), want: "post-attempt relation observation did not satisfy the refresh postcondition"},
		{name: "attempt persistence", result: failedResult(ReasonAttemptPersistence), want: "refresh attempt history could not be persisted"},
		{name: "cancelled", result: failedResult(ReasonCancelled), want: "refresh was cancelled"},
		{name: "missing reason", result: failedResult(ReasonNone), want: unavailableFailureDetail},
		{name: "unknown reason", result: failedResult(ReasonCode("private_token=secret")), want: unavailableFailureDetail},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.result.FailureDetail(); got != test.want {
				t.Fatalf("FailureDetail() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFailureDetailUsesOnlyAdmittedMechanicalAndObservationValues(t *testing.T) {
	t.Run("known command reason", func(t *testing.T) {
		result := failedResult(ReasonCommandFailed)
		result.ProcessOutcome = &ProcessOutcome{Reason: subprocess.CommandReasonNonZeroExit}
		if got, want := result.FailureDetail(), "delegated host command result: nonzero_exit"; got != want {
			t.Fatalf("FailureDetail() = %q, want %q", got, want)
		}
	})

	t.Run("unknown command reason", func(t *testing.T) {
		result := failedResult(ReasonCommandFailed)
		result.ProcessOutcome = &ProcessOutcome{Reason: subprocess.CommandReason("authCode=secret")}
		if got, want := result.FailureDetail(), "the delegated host command failed"; got != want {
			t.Fatalf("FailureDetail() = %q, want %q", got, want)
		}
	})

	t.Run("known observation", func(t *testing.T) {
		result := failedResult(ReasonPostObservationFailed)
		result.Observation = &Observation{
			State:  observerelation.StateMissing,
			Reason: observerelation.ReasonMissing,
		}
		want := "post-attempt relation observation: state=missing reason=managed_relation_missing"
		if got := result.FailureDetail(); got != want {
			t.Fatalf("FailureDetail() = %q, want %q", got, want)
		}
	})

	t.Run("unknown observation", func(t *testing.T) {
		result := failedResult(ReasonPostObservationFailed)
		result.Observation = &Observation{
			State:  observerelation.CorrelationState(`/Users/alice/private.json`),
			Reason: observerelation.ReasonCode("authorization_code=secret"),
		}
		want := "post-attempt relation observation did not satisfy the refresh postcondition"
		if got := result.FailureDetail(); got != want {
			t.Fatalf("FailureDetail() = %q, want %q", got, want)
		}
	})
}

func TestFailureDetailIsEmptyForNonErrorResults(t *testing.T) {
	for _, class := range []ResultClass{
		ResultPlanned,
		ResultAttemptedUnverified,
		ResultObservedRelation,
	} {
		result := CommandResult{
			ResultClass: class,
			ReasonCode:  ReasonCode("authorization_code=secret"),
			ProcessOutcome: &ProcessOutcome{
				Reason: subprocess.CommandReason("private_token=secret"),
			},
		}
		if got := result.FailureDetail(); got != "" {
			t.Fatalf("class %q FailureDetail() = %q, want empty", class, got)
		}
	}
}

func failedResult(reason ReasonCode) CommandResult {
	return CommandResult{ResultClass: ResultFailed, ReasonCode: reason}
}
