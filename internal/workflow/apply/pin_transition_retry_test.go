package apply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestApplyPiPinTransitionRetainsUncertaintyAndRequiresNewAttempt(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, scenario := range []string{"nonzero_old_settings", "nonzero_new_settings", "zero_old_settings", "zero_missing_settings", "zero_duplicate_settings", "cancel_after_native_change", "missing_runner"} {
			t.Run(string(scope)+"/"+scenario, func(t *testing.T) {
				fixture := newPiPinApplyFixture(t, scope)
				planning, err := PlanWrite(t.Context(), fixture.input())
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls := 0
				executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Clock: fixedApplyHostRouteClock, Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					calls++
					result := subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
					switch scenario {
					case "nonzero_old_settings":
						result.ExitCode = 23
					case "nonzero_new_settings":
						writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q]}", fixture.after))
						result.ExitCode = 23
					case "zero_missing_settings":
						writeApplyFile(t, fixture.settings, "{\"packages\":[]}")
					case "zero_duplicate_settings":
						writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q,%q]}", fixture.after, fixture.after))
					case "cancel_after_native_change":
						writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q]}", fixture.after))
						cancel()
					case "missing_runner":
						result = subprocess.CommandResult{MissingRunner: true, Err: errors.New("fixture runner unavailable")}
					}
					return result
				}})
				if _, err := ExecuteWithOptions(ctx, planning, ExecuteOptions{HostRouteExecutor: executor}); err == nil {
					t.Fatal("uncertain transition reported success")
				}
				if calls != 1 {
					t.Fatalf("native calls = %d, want one without implicit retry", calls)
				}
				claims := loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, scope)
				if len(claims) != 1 {
					t.Fatalf("claims after failure = %d", len(claims))
				}
				pair, pending := claims[0].PendingPinTransition()
				if !pending || !pair.Before().ExactEqual(fixture.claim) {
					t.Fatal("failed transition lost old management or intent")
				}

				if scenario == "zero_missing_settings" || scenario == "zero_duplicate_settings" {
					_, err := PlanWrite(t.Context(), fixture.input())
					if err == nil || !strings.Contains(err.Error(), "pin_transition_conflict") {
						t.Fatalf("ambiguous or missing settings retry = %v", err)
					}
					writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q]}", fixture.after))
				}
				retry, err := PlanWrite(t.Context(), fixture.input())
				if err != nil {
					t.Fatal(err)
				}
				action := retry.Reconciliation.Relations()[0]
				if action.Kind() != reconcile.ActionChangePin || !action.ResumesPinTransition() {
					t.Fatalf("retry action = %s, resume=%t", action.Kind(), action.ResumesPinTransition())
				}
				claims = loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, scope)
				if _, pending := claims[0].PendingPinTransition(); !pending {
					t.Fatal("later desired settings silently completed management")
				}
				executor = subprocess.NewCommandExecutor(subprocess.CommandOptions{Clock: fixedApplyHostRouteClock, Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					calls++
					writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q]}", fixture.after))
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				}})
				if _, err := ExecuteWithOptions(t.Context(), retry, ExecuteOptions{HostRouteExecutor: executor}); err != nil {
					t.Fatal(err)
				}
				if calls != 2 {
					t.Fatalf("same-target retry skipped native attempt: calls=%d", calls)
				}
				claims = loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, scope)
				if !claims[0].MatchesLockedRecord(fixture.locked.Locked.Subjects()[0]) {
					t.Fatal("authorized retry did not complete management")
				}
			})
		}
	}
}
