package runtimeprobe

import (
	"strings"
	"testing"
)

func TestFoldFactsKeepsMissingDimensionsNotProbed(t *testing.T) {
	observation, err := FoldFacts(nil)
	if err != nil {
		t.Fatalf("FoldFacts returned error: %v", err)
	}

	want := ReadinessObservation{
		state:  NotProbed,
		reason: ReasonNotProbed,
	}
	for name, got := range map[string]ReadinessObservation{
		string(DimensionLauncher):           observation.Launcher(),
		string(DimensionProtocolInitialize): observation.ProtocolInitialize(),
		string(DimensionAuthentication):     observation.Authentication(),
		string(DimensionEndpointHealth):     observation.EndpointHealth(),
		string(DimensionToolInventory):      observation.ToolInventory(),
	} {
		if got != want {
			t.Fatalf("%s = %#v, want %#v", name, got, want)
		}
	}
}

func TestObservationZeroValueIsNotProbedAndNonFailing(t *testing.T) {
	var observation Observation
	for name, got := range map[string]ReadinessObservation{
		string(DimensionLauncher):           observation.Launcher(),
		string(DimensionProtocolInitialize): observation.ProtocolInitialize(),
		string(DimensionAuthentication):     observation.Authentication(),
		string(DimensionEndpointHealth):     observation.EndpointHealth(),
		string(DimensionToolInventory):      observation.ToolInventory(),
	} {
		if got.State() != NotProbed || got.Reason() != ReasonNotProbed || got.IsFailure() {
			t.Fatalf("%s zero readiness = %#v, want non-failing not_probed", name, got)
		}
	}
}

func TestReadinessObservationOwnsFailureClassification(t *testing.T) {
	tests := []struct {
		state       Readiness
		wantFailure bool
	}{
		{state: NotProbed},
		{state: NotApplicable},
		{state: ObservedOK},
		{state: Unsupported, wantFailure: true},
		{state: ObservedFailed, wantFailure: true},
		{state: Blocked, wantFailure: true},
		{state: Stale, wantFailure: true},
	}

	for _, test := range tests {
		observation := ReadinessObservation{state: test.state}
		if got := observation.IsFailure(); got != test.wantFailure {
			t.Errorf("state %q IsFailure = %t, want %t", test.state, got, test.wantFailure)
		}
	}
}

func TestFoldFactsFoldsClosedRuntimeStates(t *testing.T) {
	runtimeObservation, err := FoldFacts([]Fact{
		{
			Dimension: DimensionLauncher,
			State:     ObservedOK,
			Source:    SourceExplicit,
			Freshness: FreshnessCurrent,
		},
		{
			Dimension:       DimensionProtocolInitialize,
			State:           ObservedFailed,
			Reason:          ReasonObservedFailed,
			Source:          SourceExplicit,
			Freshness:       FreshnessCurrent,
			SanitizedDetail: "redacted initialize failure",
		},
		{
			Dimension: DimensionAuthentication,
			State:     NotApplicable,
			Reason:    ReasonNotApplicable,
			Source:    SourceAssisted,
			Freshness: FreshnessCurrent,
		},
		{
			Dimension: DimensionEndpointHealth,
			State:     Unsupported,
			Reason:    ReasonUnsupported,
			Source:    SourceAssisted,
			Freshness: FreshnessCurrent,
		},
		{
			Dimension:       DimensionToolInventory,
			State:           Stale,
			Reason:          ReasonStale,
			Source:          SourceExplicit,
			Freshness:       FreshnessStale,
			SanitizedDetail: "redacted stale tool inventory",
		},
	})
	if err != nil {
		t.Fatalf("FoldFacts returned error: %v", err)
	}

	if runtimeObservation.Launcher().State() != ObservedOK ||
		runtimeObservation.Launcher().Reason() != ReasonNone ||
		runtimeObservation.Launcher().Source() != SourceExplicit ||
		runtimeObservation.Launcher().Freshness() != FreshnessCurrent {
		t.Fatalf("launcher = %#v, want observed current explicit success", runtimeObservation.Launcher())
	}
	if runtimeObservation.ProtocolInitialize().State() != ObservedFailed ||
		runtimeObservation.ProtocolInitialize().Reason() != ReasonObservedFailed ||
		runtimeObservation.ProtocolInitialize().SanitizedDetail() != "redacted initialize failure" {
		t.Fatalf("protocol initialize = %#v, want redacted observed failure", runtimeObservation.ProtocolInitialize())
	}
	if runtimeObservation.Authentication().State() != NotApplicable ||
		runtimeObservation.Authentication().Reason() != ReasonNotApplicable {
		t.Fatalf("authentication = %#v, want not_applicable", runtimeObservation.Authentication())
	}
	if runtimeObservation.EndpointHealth().State() != Unsupported ||
		runtimeObservation.EndpointHealth().Reason() != ReasonUnsupported {
		t.Fatalf("endpoint health = %#v, want unsupported", runtimeObservation.EndpointHealth())
	}
	if runtimeObservation.ToolInventory().State() != Stale ||
		runtimeObservation.ToolInventory().Freshness() != FreshnessStale ||
		runtimeObservation.ToolInventory().Reason() != ReasonStale {
		t.Fatalf("tool inventory = %#v, want stale", runtimeObservation.ToolInventory())
	}
}

func TestFoldFactsAcceptsBlockedRuntimeFact(t *testing.T) {
	runtimeObservation, err := FoldFacts([]Fact{
		{
			Dimension:       DimensionLauncher,
			State:           Blocked,
			Reason:          ReasonBlocked,
			Source:          SourceExplicit,
			Freshness:       FreshnessCurrent,
			SanitizedDetail: "redacted user denied launch probe",
		},
	})
	if err != nil {
		t.Fatalf("FoldFacts returned error: %v", err)
	}
	if runtimeObservation.Launcher().State() != Blocked ||
		runtimeObservation.Launcher().Reason() != ReasonBlocked ||
		runtimeObservation.Launcher().SanitizedDetail() != "redacted user denied launch probe" {
		t.Fatalf("launcher = %#v, want blocked explicit fact", runtimeObservation.Launcher())
	}
}

func TestFoldFactsRejectsAmbiguousRuntimeFacts(t *testing.T) {
	_, err := FoldFacts([]Fact{
		{
			Dimension: DimensionLauncher,
			State:     ObservedOK,
			Source:    SourceExplicit,
			Freshness: FreshnessCurrent,
		},
		{
			Dimension: DimensionLauncher,
			State:     ObservedFailed,
			Reason:    ReasonObservedFailed,
			Source:    SourceExplicit,
			Freshness: FreshnessCurrent,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate runtime readiness fact") {
		t.Fatalf("FoldFacts error = %v, want duplicate dimension rejection", err)
	}
}

func TestFoldFactsRejectsInvalidRuntimeFacts(t *testing.T) {
	tests := []struct {
		name string
		fact Fact
		want string
	}{
		{
			name: "unsupported dimension",
			fact: Fact{
				Dimension: Dimension("transport_tls"),
				State:     ObservedOK,
				Source:    SourceExplicit,
				Freshness: FreshnessCurrent,
			},
			want: "unsupported runtime readiness dimension",
		},
		{
			name: "unsupported state",
			fact: Fact{
				Dimension: DimensionLauncher,
				State:     Readiness("half_ready"),
				Source:    SourceExplicit,
				Freshness: FreshnessCurrent,
			},
			want: "unsupported runtime readiness state",
		},
		{
			name: "unsupported source",
			fact: Fact{
				Dimension: DimensionLauncher,
				State:     ObservedOK,
				Source:    Source("background_poll"),
				Freshness: FreshnessCurrent,
			},
			want: "unsupported runtime probe source",
		},
		{
			name: "unsupported freshness",
			fact: Fact{
				Dimension: DimensionLauncher,
				State:     ObservedOK,
				Source:    SourceExplicit,
				Freshness: Freshness("unknown"),
			},
			want: "unsupported runtime freshness",
		},
		{
			name: "not probed fact",
			fact: Fact{
				Dimension: DimensionLauncher,
				State:     NotProbed,
				Reason:    ReasonNotProbed,
				Source:    SourceExplicit,
				Freshness: FreshnessCurrent,
			},
			want: "must not use",
		},
		{
			name: "success with reason",
			fact: Fact{
				Dimension: DimensionLauncher,
				State:     ObservedOK,
				Reason:    ReasonObservedFailed,
				Source:    SourceExplicit,
				Freshness: FreshnessCurrent,
			},
			want: "requires empty reason",
		},
		{
			name: "success with detail",
			fact: Fact{
				Dimension:       DimensionLauncher,
				State:           ObservedOK,
				Source:          SourceExplicit,
				Freshness:       FreshnessCurrent,
				SanitizedDetail: "should not be stored on success",
			},
			want: "must not carry sanitized detail",
		},
		{
			name: "failure without matching reason",
			fact: Fact{
				Dimension: DimensionProtocolInitialize,
				State:     ObservedFailed,
				Source:    SourceExplicit,
				Freshness: FreshnessCurrent,
			},
			want: "requires reason",
		},
		{
			name: "not applicable with wrong reason",
			fact: Fact{
				Dimension: DimensionAuthentication,
				State:     NotApplicable,
				Reason:    ReasonUnsupported,
				Source:    SourceAssisted,
				Freshness: FreshnessCurrent,
			},
			want: "requires reason",
		},
		{
			name: "stale state with current freshness",
			fact: Fact{
				Dimension: DimensionToolInventory,
				State:     Stale,
				Reason:    ReasonStale,
				Source:    SourceExplicit,
				Freshness: FreshnessCurrent,
			},
			want: "is stale but freshness",
		},
		{
			name: "stale freshness with current state",
			fact: Fact{
				Dimension: DimensionEndpointHealth,
				State:     Unsupported,
				Reason:    ReasonUnsupported,
				Source:    SourceAssisted,
				Freshness: FreshnessStale,
			},
			want: "stale freshness with non-stale state",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := FoldFacts([]Fact{test.fact})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("FoldFacts error = %v, want containing %q", err, test.want)
			}
		})
	}
}
