package reconcile_test

import (
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	reconcile "github.com/isty2e/daem/internal/reconcile"
)

func TestResultMissingConsumesBlockedOrdinaryHostDelegatedAndAssistedAdmission(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	missing := correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})

	tests := []struct {
		name      string
		admission reconcile.RelationRouteAdmissionDecision
		kind      reconcile.RelationActionKind
		execution reconcile.RelationExecutionClass
		reason    reconcile.RelationReasonCode
		invokes   bool
		blocks    bool
	}{
		{
			name:      "blocked",
			admission: blockedAdmission(t),
			kind:      reconcile.ActionCreate,
			execution: reconcile.ExecutionBlocked,
			reason:    reconcile.ReasonRouteNotAdmitted,
			blocks:    true,
		},
		{
			name:      "ordinary mutation",
			admission: ordinaryAdmission(t),
			kind:      reconcile.ActionCreate,
			execution: reconcile.ExecutionHostRoute,
			reason:    reconcile.ReasonNone,
			invokes:   true,
		},
		{
			name:      "host delegated",
			admission: hostDelegatedAdmission(t),
			kind:      reconcile.ActionCreate,
			execution: reconcile.ExecutionHostRoute,
			reason:    reconcile.ReasonNone,
			invokes:   true,
		},
		{
			name:      "assisted",
			admission: assistedAdmission(t),
			kind:      reconcile.ActionAssistCandidate,
			execution: reconcile.ExecutionAssisted,
			reason:    reconcile.ReasonRouteRequiresAssistance,
			blocks:    true,
		},
		{
			name:      "explicit attempt",
			admission: explicitAttemptAdmission(t),
			kind:      reconcile.ActionAssistCandidate,
			execution: reconcile.ExecutionAssisted,
			reason:    reconcile.ReasonRouteRequiresAssistance,
			blocks:    true,
		},
		{
			name:      "observe only",
			admission: observeOnlyAdmission(t),
			kind:      reconcile.ActionObserveOnly,
			execution: reconcile.ExecutionObserveOnly,
			reason:    reconcile.ReasonUnsupportedPassiveInventory,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := mustPlan(t, subject, missing, tt.admission)
			assertAction(t, action, tt.kind, tt.execution, tt.reason)
			if action.InvokesHostRoute() != tt.invokes {
				t.Fatalf("InvokesHostRoute = %t, want %t", action.InvokesHostRoute(), tt.invokes)
			}
			if action.BlocksOrdinaryApply() != tt.blocks {
				t.Fatalf("BlocksOrdinaryApply = %t, want %t", action.BlocksOrdinaryApply(), tt.blocks)
			}
		})
	}
}

func TestHostDelegatedAndOrdinaryAdmissionRemainDistinct(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	missing := correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})

	ordinary := mustPlan(t, subject, missing, ordinaryAdmission(t))
	hostDelegated := mustPlan(t, subject, missing, hostDelegatedAdmission(t))

	for _, action := range []reconcile.RelationAction{ordinary, hostDelegated} {
		assertAction(t, action, reconcile.ActionCreate, reconcile.ExecutionHostRoute, reconcile.ReasonNone)
		if !action.InvokesHostRoute() {
			t.Fatalf("host-route action with selected outcome %q did not invoke host route", action.RouteAdmission().SelectedOutcome())
		}
		if action.BlocksOrdinaryApply() {
			t.Fatalf("host-route action with selected outcome %q blocked ordinary apply", action.RouteAdmission().SelectedOutcome())
		}
	}
	if ordinary.RouteAdmission().SelectedOutcome() != reconcile.AdmissionOutcomeOrdinaryMutation {
		t.Fatalf("ordinary selected outcome = %q", ordinary.RouteAdmission().SelectedOutcome())
	}
	if hostDelegated.RouteAdmission().SelectedOutcome() != reconcile.AdmissionOutcomeHostDelegated {
		t.Fatalf("host delegated selected outcome = %q", hostDelegated.RouteAdmission().SelectedOutcome())
	}
	if ordinary.RouteAdmission().SelectedOutcome() == hostDelegated.RouteAdmission().SelectedOutcome() {
		t.Fatalf("ordinary and host-delegated outcomes collapsed to %q", ordinary.RouteAdmission().SelectedOutcome())
	}
}

func TestHostDelegatedKeepsConflictAndObservationPrecedence(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	tests := []struct {
		name      string
		spec      observerelation.InventorySpec
		kind      reconcile.RelationActionKind
		execution reconcile.RelationExecutionClass
		reason    reconcile.RelationReasonCode
	}{
		{
			name: "fresh exact managed no-op",
			spec: observerelation.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observerelation.Row{
					mustManagedRow(t, "context7", "managed/context7"),
				},
			},
			kind:      reconcile.ActionNoOp,
			execution: reconcile.ExecutionNoMutation,
			reason:    reconcile.ReasonNone,
		},
		{
			name: "unmanaged same subject blocks",
			spec: observerelation.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observerelation.Row{
					mustUnmanagedRow(t, "context7"),
				},
			},
			kind:      reconcile.ActionBlock,
			execution: reconcile.ExecutionBlocked,
			reason:    reconcile.ReasonUnkeyedSameSubject,
		},
		{
			name: "stale exact looking row blocks",
			spec: observerelation.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceStale,
				Rows: []observerelation.Row{
					mustManagedRow(t, "context7", "managed/context7"),
				},
			},
			kind:      reconcile.ActionBlock,
			execution: reconcile.ExecutionBlocked,
			reason:    reconcile.ReasonStaleEvidence,
		},
		{
			name: "unsupported inventory observes only",
			spec: observerelation.InventorySpec{
				Availability: observerelation.InventoryUnsupported,
				Freshness:    observerelation.EvidenceFresh,
			},
			kind:      reconcile.ActionObserveOnly,
			execution: reconcile.ExecutionObserveOnly,
			reason:    reconcile.ReasonUnsupportedPassiveInventory,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := mustPlan(t, subject, correlationFor(t, subject, tt.spec), hostDelegatedAdmission(t))
			assertAction(t, action, tt.kind, tt.execution, tt.reason)
			if action.InvokesHostRoute() {
				t.Fatalf("%s must not invoke host route", tt.name)
			}
		})
	}
}

func TestHostDelegatedPreservesRouteRequestIdentityWithoutArtifactClaim(t *testing.T) {
	subject := mustSubject(t, "sha256-context7", "managed/sha256-context7")
	missing := correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	input := validInput(t, subject, missing, hostDelegatedAdmission(t))
	routeRequest, err := realizationdelegate.NewRequest(
		"sha256-looking.plugin.install",
		"test-v1",
		"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatalf("NewDelegatedRouteRequest: %v", err)
	}
	input.RouteRequest = routeRequest

	action, err := reconcile.NewRelationAction(input)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	assertAction(t, action, reconcile.ActionCreate, reconcile.ExecutionHostRoute, reconcile.ReasonNone)
	if action.RouteRequest().CanonicalRequestHash() != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("route request hash = %q", action.RouteRequest().CanonicalRequestHash())
	}
	if action.RouteAdmission().SelectedOutcome() != reconcile.AdmissionOutcomeHostDelegated {
		t.Fatalf("selected outcome = %q, want host-delegated", action.RouteAdmission().SelectedOutcome())
	}
}

func TestAssistedAndExplicitAttemptRemainNonNormalApplyPaths(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	missing := correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})

	for _, admission := range []reconcile.RelationRouteAdmissionDecision{
		assistedAdmission(t),
		explicitAttemptAdmission(t),
	} {
		action := mustPlan(t, subject, missing, admission)
		assertAction(t, action, reconcile.ActionAssistCandidate, reconcile.ExecutionAssisted, reconcile.ReasonRouteRequiresAssistance)
		if action.InvokesHostRoute() {
			t.Fatalf("selected outcome %q became a normal host-route invocation", admission.SelectedOutcome())
		}
		if !action.BlocksOrdinaryApply() {
			t.Fatalf("selected outcome %q did not block ordinary apply", admission.SelectedOutcome())
		}
	}
}

func TestResultUnsupportedInventoryIsObserveOnly(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	action := mustPlan(t, subject, correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventoryUnsupported,
		Freshness:    observerelation.EvidenceFresh,
	}), ordinaryAdmission(t))

	assertAction(t, action, reconcile.ActionObserveOnly, reconcile.ExecutionObserveOnly, reconcile.ReasonUnsupportedPassiveInventory)
	assertEvidence(t, action, observerelation.InventoryUnsupported, observerelation.EvidenceFresh)
	assertWatchpoints(t, action, []observerelation.Watchpoint{observerelation.WatchpointPassiveInventoryRequired})
	if action.BlocksOrdinaryApply() {
		t.Fatalf("unsupported observe-only action should not block ordinary apply")
	}
}

func TestResultUnsupportedInventoryAttemptsOnlyUnderExplicitPolicy(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	unsupported := correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventoryUnsupported,
		Freshness:    observerelation.EvidenceFresh,
	})

	action := mustPlan(t, subject, unsupported, attemptWhenUnsupportedAdmission(t))
	assertAction(t, action, reconcile.ActionAttempt, reconcile.ExecutionHostRoute, reconcile.ReasonUnsupportedPassiveInventory)
	assertEvidence(t, action, observerelation.InventoryUnsupported, observerelation.EvidenceFresh)
	assertWatchpoints(t, action, []observerelation.Watchpoint{observerelation.WatchpointPassiveInventoryRequired})
	if !action.InvokesHostRoute() || action.BlocksOrdinaryApply() {
		t.Fatalf("unsupported attempt = invokes %t blocks %t, want executable ordinary host route", action.InvokesHostRoute(), action.BlocksOrdinaryApply())
	}
}

func TestAttemptWhenUnsupportedDoesNotAdmitUnavailableEvidence(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	action := mustPlan(t, subject, correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventoryUnavailable,
		Freshness:    observerelation.EvidenceFresh,
	}), attemptWhenUnsupportedAdmission(t))

	assertAction(t, action, reconcile.ActionObserveOnly, reconcile.ExecutionObserveOnly, reconcile.ReasonRelationEvidenceUnavailable)
	if action.InvokesHostRoute() {
		t.Fatal("unavailable evidence fell through to unsupported-observer attempt")
	}
}

func TestAttemptWhenUnsupportedDoesNotBypassStaleOrConflictingEvidence(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	tests := []struct {
		name   string
		spec   observerelation.InventorySpec
		reason reconcile.RelationReasonCode
	}{
		{
			name: "stale exact-looking evidence",
			spec: observerelation.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceStale,
				Rows:         []observerelation.Row{mustManagedRow(t, "context7", "managed/context7")},
			},
			reason: reconcile.ReasonStaleEvidence,
		},
		{
			name: "unmanaged same subject",
			spec: observerelation.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         []observerelation.Row{mustUnmanagedRow(t, "context7")},
			},
			reason: reconcile.ReasonUnkeyedSameSubject,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := mustPlan(t, subject, correlationFor(t, subject, tt.spec), attemptWhenUnsupportedAdmission(t))
			assertAction(t, action, reconcile.ActionBlock, reconcile.ExecutionBlocked, tt.reason)
			if action.InvokesHostRoute() || !action.BlocksOrdinaryApply() {
				t.Fatalf("action = invokes %t blocks %t, want blocked", action.InvokesHostRoute(), action.BlocksOrdinaryApply())
			}
		})
	}
}

func TestResultUnavailableInventoryIsObserveOnlyEvidenceGap(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	action := mustPlan(t, subject, correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventoryUnavailable,
		Freshness:    observerelation.EvidenceFresh,
	}), ordinaryAdmission(t))

	assertAction(t, action, reconcile.ActionObserveOnly, reconcile.ExecutionObserveOnly, reconcile.ReasonRelationEvidenceUnavailable)
	assertEvidence(t, action, observerelation.InventoryUnavailable, observerelation.EvidenceFresh)
	assertWatchpoints(t, action, []observerelation.Watchpoint{observerelation.WatchpointRelationEvidenceRequired})
	if action.InvokesHostRoute() {
		t.Fatalf("unavailable relation evidence must not invoke host route")
	}
	if action.BlocksOrdinaryApply() {
		t.Fatalf("unavailable observe-only action should not block ordinary apply")
	}
}
