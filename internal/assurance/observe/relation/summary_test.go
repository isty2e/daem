package relation

import "testing"

func TestParseRelationSummariesNormalizesOmittedValuesAndRejectsUnknownValues(t *testing.T) {
	observation, err := ParseObservationSummary("")
	if err != nil {
		t.Fatal(err)
	}
	if observation != ObservationNotObserved {
		t.Fatalf("omitted observation summary = %q, want %q", observation, ObservationNotObserved)
	}

	postcondition, err := ParsePostconditionSummary("")
	if err != nil {
		t.Fatal(err)
	}
	if postcondition != PostconditionNotObserved {
		t.Fatalf("omitted postcondition summary = %q, want %q", postcondition, PostconditionNotObserved)
	}

	if _, err := ParseObservationSummary("ready"); err == nil {
		t.Fatal("unknown observation summary accepted")
	}
	if _, err := ParsePostconditionSummary("installed"); err == nil {
		t.Fatal("unknown postcondition summary accepted")
	}
}

func TestRelationSummariesPreserveUnknownInsteadOfClaimingCurrentTruth(t *testing.T) {
	uncertainStates := []CorrelationState{
		StateUnkeyedSameSubject,
		StateSameSubjectShadow,
		StateManagedKeyDrift,
		StateAmbiguous,
		StateStaleEvidence,
		StateUnsupported,
		StateUnavailableEvidence,
	}
	for _, state := range uncertainStates {
		if got := SummarizeObservation(state); got != ObservationUnknown {
			t.Errorf("observation summary for %q = %q, want %q", state, got, ObservationUnknown)
		}
		if got := SummarizePostcondition(state); got != PostconditionUnknown {
			t.Errorf("postcondition summary for %q = %q, want %q", state, got, PostconditionUnknown)
		}
	}
}

func TestRelationSummariesDistinguishPresenceAbsenceAndNoEvidence(t *testing.T) {
	cases := []struct {
		name          string
		state         CorrelationState
		observation   ObservationSummary
		postcondition PostconditionSummary
	}{
		{
			name:          "present",
			state:         StateExactCorrelation,
			observation:   ObservationPresent,
			postcondition: PostconditionObserved,
		},
		{
			name:          "missing",
			state:         StateMissing,
			observation:   ObservationMissing,
			postcondition: PostconditionMissing,
		},
		{
			name:          "zero",
			observation:   ObservationNotObserved,
			postcondition: PostconditionNotObserved,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := SummarizeObservation(test.state); got != test.observation {
				t.Fatalf("observation summary = %q, want %q", got, test.observation)
			}
			if got := SummarizePostcondition(test.state); got != test.postcondition {
				t.Fatalf("postcondition summary = %q, want %q", got, test.postcondition)
			}
		})
	}
}
