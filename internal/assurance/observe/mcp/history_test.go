package mcp

import "testing"

func TestLastDelegateAttemptObservationFromInputClassifiesHistoryOnly(t *testing.T) {
	tests := []struct {
		name       string
		input      LastDelegateAttemptInput
		wantState  DelegateAttemptState
		wantReason ReasonCode
	}{
		{
			name:       "not observed",
			wantState:  DelegateAttemptNotObserved,
			wantReason: ReasonLastDelegateAttemptUnobserved,
		},
		{
			name: "different plan identity",
			input: LastDelegateAttemptInput{
				Observed: true,
			},
			wantState:  DelegateAttemptStale,
			wantReason: ReasonLastDelegateAttemptStale,
		},
		{
			name: "matching success",
			input: LastDelegateAttemptInput{
				Observed:            true,
				MatchesPlanIdentity: true,
				Status:              DelegateAttemptSucceeded,
			},
			wantState: DelegateAttemptSucceeded,
		},
		{
			name: "matching failure",
			input: LastDelegateAttemptInput{
				Observed:            true,
				MatchesPlanIdentity: true,
				Status:              DelegateAttemptFailed,
				Reason:              ReasonDelegateNonZeroExit,
			},
			wantState:  DelegateAttemptFailed,
			wantReason: ReasonDelegateNonZeroExit,
		},
		{
			name: "matching blocked",
			input: LastDelegateAttemptInput{
				Observed:            true,
				MatchesPlanIdentity: true,
				Status:              DelegateAttemptBlocked,
				Reason:              ReasonDelegatePolicyBlocked,
			},
			wantState:  DelegateAttemptBlocked,
			wantReason: ReasonDelegatePolicyBlocked,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation, err := LastDelegateAttemptObservationFromInput(test.input)
			if err != nil {
				t.Fatalf("LastDelegateAttemptObservationFromInput returned error: %v", err)
			}
			if observation.State != test.wantState || observation.Reason != test.wantReason {
				t.Fatalf(
					"last delegate attempt = %#v, want %q/%q",
					observation,
					test.wantState,
					test.wantReason,
				)
			}
		})
	}
}

func TestLastDelegateAttemptObservationFromInputRejectsContradictoryFacts(t *testing.T) {
	tests := []LastDelegateAttemptInput{
		{MatchesPlanIdentity: true},
		{Observed: true, Status: DelegateAttemptSucceeded},
		{Observed: true, MatchesPlanIdentity: true},
		{
			Observed:            true,
			MatchesPlanIdentity: true,
			Status:              DelegateAttemptSucceeded,
			Reason:              ReasonDelegateNonZeroExit,
		},
		{
			Observed:            true,
			MatchesPlanIdentity: true,
			Status:              DelegateAttemptFailed,
		},
		{
			Observed:            true,
			MatchesPlanIdentity: true,
			Status:              DelegateAttemptBlocked,
			Reason:              ReasonDelegateTimeout,
		},
	}

	for index, input := range tests {
		if _, err := LastDelegateAttemptObservationFromInput(input); err == nil {
			t.Fatalf("input %d returned nil error", index)
		}
	}
}
