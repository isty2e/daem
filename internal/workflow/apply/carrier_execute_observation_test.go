package apply

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestExecuteWithOptionsRecordsObservedAbsentAfterSuccessfulClaudePluginHostRoute(t *testing.T) {
	tests := []struct {
		name         string
		scope        target.Scope
		hostScopeArg string
	}{
		{
			name:         "project",
			scope:        target.ScopeProject,
			hostScopeArg: "project",
		},
		{
			name:         "global",
			scope:        target.ScopeGlobal,
			hostScopeArg: "user",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, test.scope)
			var requests []subprocess.CommandRequest
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					requests = append(requests, request)
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			})
			observer := func(ctx context.Context, command executehostroute.Command, _ []durablecarrier.PendingCarrierInstall, _ []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
				return assurancehostroute.CurrentObservation(observeclaudeplugin.Correlate(subject, applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
					Availability: observerelation.InventorySupported,
					Freshness:    observerelation.EvidenceFresh,
				})))
			}

			planning, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &missingInventory,
			})
			if err != nil {
				t.Fatalf("PlanWrite returned error: %v", err)
			}
			result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
				RelationObservations: &missingInventory,
				HostRouteExecutor:    executor,
				HostRouteObserver:    observer,
			})
			if err == nil {
				t.Fatal("ExecuteWithOptions returned nil error for absent post-attempt observation")
			}
			if !strings.Contains(err.Error(), "attempted_observed_absent/observed_absent") {
				t.Fatalf("ExecuteWithOptions error = %v, want attempted observed absent", err)
			}

			assertApplyClaudeHostRouteCommandRequest(t, root, requests, test.hostScopeArg)
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedObservedAbsent, "observed_absent", false)
			if result.HostRouteAttempts[0].ObservationSummary() != observerelation.ObservationMissing ||
				result.HostRouteAttempts[0].PostconditionSummary() != observerelation.PostconditionMissing {
				t.Fatalf("result host route attempt = %#v, want missing observation and postcondition", result.HostRouteAttempts[0])
			}
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedObservedAbsent, "observed_absent", false)
			assertApplyNoCarrierFact(t, state, locked.Locked.Subjects()[0].SubjectID())
			if state.HostRouteAttempts()[0].ObservationSummary() != observerelation.ObservationMissing ||
				state.HostRouteAttempts()[0].PostconditionSummary() != observerelation.PostconditionMissing {
				t.Fatalf("state host route attempt = %#v, want missing observation and postcondition", state.HostRouteAttempts()[0])
			}
		})
	}
}

func TestExecuteWithOptionsRejectsWrongScopePostAttemptObservation(t *testing.T) {
	tests := []struct {
		name         string
		scope        target.Scope
		hostScopeArg string
		rowScope     observeclaudeplugin.HostScope
	}{
		{
			name:         "project desired sees host user row",
			scope:        target.ScopeProject,
			hostScopeArg: "project",
			rowScope:     observeclaudeplugin.HostScopeUser,
		},
		{
			name:         "global desired sees host project row",
			scope:        target.ScopeGlobal,
			hostScopeArg: "user",
			rowScope:     observeclaudeplugin.HostScopeProject,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, test.scope)
			var requests []subprocess.CommandRequest
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					requests = append(requests, request)
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			})
			observer := func(ctx context.Context, command executehostroute.Command, _ []durablecarrier.PendingCarrierInstall, _ []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
				return assurancehostroute.CurrentObservation(observeclaudeplugin.Correlate(subject, applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
					Availability: observerelation.InventorySupported,
					Freshness:    observerelation.EvidenceFresh,
					Rows: []observeclaudeplugin.Row{
						applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), test.rowScope),
					},
				})))
			}

			planning, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &missingInventory,
			})
			if err != nil {
				t.Fatalf("PlanWrite returned error: %v", err)
			}
			result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
				RelationObservations: &missingInventory,
				HostRouteExecutor:    executor,
				HostRouteObserver:    observer,
			})
			if err == nil {
				t.Fatal("ExecuteWithOptions returned nil error for wrong-scope post-attempt observation")
			}
			if !strings.Contains(err.Error(), "attempted_observed_absent/observed_absent") {
				t.Fatalf("ExecuteWithOptions error = %v, want attempted observed absent", err)
			}

			assertApplyClaudeHostRouteCommandRequest(t, root, requests, test.hostScopeArg)
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedObservedAbsent, "observed_absent", false)
			if result.HostRouteAttempts[0].ObservationSummary() != observerelation.ObservationMissing ||
				result.HostRouteAttempts[0].PostconditionSummary() != observerelation.PostconditionMissing {
				t.Fatalf("result host route attempt = %#v, want missing observation and postcondition", result.HostRouteAttempts[0])
			}
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedObservedAbsent, "observed_absent", false)
			if state.HostRouteAttempts()[0].ObservationSummary() != observerelation.ObservationMissing ||
				state.HostRouteAttempts()[0].PostconditionSummary() != observerelation.PostconditionMissing {
				t.Fatalf("state host route attempt = %#v, want missing observation and postcondition", state.HostRouteAttempts()[0])
			}
		})
	}
}

func TestExecuteWithOptionsRecordsAttemptedUnverifiedWithoutObserver(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			})
			planning, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &missingInventory,
			})
			if err != nil {
				t.Fatalf("PlanWrite returned error: %v", err)
			}
			result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
				RelationObservations: &missingInventory,
				HostRouteExecutor:    executor,
			})
			if err != nil {
				t.Fatalf("ExecuteWithOptions returned error: %v", err)
			}
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
			assertApplyNoCarrierFact(t, state, locked.Locked.Subjects()[0].SubjectID())
		})
	}
}

func TestExecuteWithOptionsRecordsAttemptedUnverifiedPostAttemptObserverFailures(t *testing.T) {
	tests := []struct {
		name              string
		scope             target.Scope
		hostScopeArg      string
		rowScope          observeclaudeplugin.HostScope
		unavailableReason assurancehostroute.ResultReasonCode
		wantReason        durableattempt.HostRouteResultReason
		wantObservation   observerelation.ObservationSummary
	}{
		{
			name:            "project stale scoped row",
			scope:           target.ScopeProject,
			hostScopeArg:    "project",
			rowScope:        observeclaudeplugin.HostScopeProject,
			wantReason:      "observation_stale",
			wantObservation: observerelation.ObservationUnknown,
		},
		{
			name:            "global stale scoped row",
			scope:           target.ScopeGlobal,
			hostScopeArg:    "user",
			rowScope:        observeclaudeplugin.HostScopeUser,
			wantReason:      "observation_stale",
			wantObservation: observerelation.ObservationUnknown,
		},
		{
			name:              "project observer parse failure",
			scope:             target.ScopeProject,
			hostScopeArg:      "project",
			unavailableReason: assurancehostroute.ResultReasonObservationParseFailed,
			wantReason:        "observation_parse_failed",
			wantObservation:   observerelation.ObservationNotObserved,
		},
		{
			name:              "global observer parse failure",
			scope:             target.ScopeGlobal,
			hostScopeArg:      "user",
			unavailableReason: assurancehostroute.ResultReasonObservationParseFailed,
			wantReason:        "observation_parse_failed",
			wantObservation:   observerelation.ObservationNotObserved,
		},
		{
			name:              "project observer unavailable",
			scope:             target.ScopeProject,
			hostScopeArg:      "project",
			unavailableReason: assurancehostroute.ResultReasonObservationUnavailable,
			wantReason:        "observation_unavailable",
			wantObservation:   observerelation.ObservationNotObserved,
		},
		{
			name:              "global observer unavailable",
			scope:             target.ScopeGlobal,
			hostScopeArg:      "user",
			unavailableReason: assurancehostroute.ResultReasonObservationUnavailable,
			wantReason:        "observation_unavailable",
			wantObservation:   observerelation.ObservationNotObserved,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, test.scope)
			var requests []subprocess.CommandRequest
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					requests = append(requests, request)
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			})
			observer := func(ctx context.Context, command executehostroute.Command, _ []durablecarrier.PendingCarrierInstall, _ []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
				if test.unavailableReason != "" {
					return assurancehostroute.ObservationUnavailable(test.unavailableReason)
				}
				return assurancehostroute.CurrentObservation(observeclaudeplugin.Correlate(subject, applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
					Availability: observerelation.InventorySupported,
					Freshness:    observerelation.EvidenceStale,
					Rows: []observeclaudeplugin.Row{
						applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), test.rowScope),
					},
				})))
			}

			planning, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &missingInventory,
			})
			if err != nil {
				t.Fatalf("PlanWrite returned error: %v", err)
			}
			result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
				RelationObservations: &missingInventory,
				HostRouteExecutor:    executor,
				HostRouteObserver:    observer,
			})
			if err != nil {
				t.Fatalf("ExecuteWithOptions returned error: %v", err)
			}

			assertApplyClaudeHostRouteCommandRequest(t, root, requests, test.hostScopeArg)
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedUnverified, test.wantReason, false)
			if result.HostRouteAttempts[0].ObservationSummary() != test.wantObservation ||
				result.HostRouteAttempts[0].PostconditionSummary() != observerelation.PostconditionUnknown {
				t.Fatalf("result host route attempt = %#v, want observation %q and unknown postcondition", result.HostRouteAttempts[0], test.wantObservation)
			}
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedUnverified, test.wantReason, false)
			if state.HostRouteAttempts()[0].ObservationSummary() != test.wantObservation ||
				state.HostRouteAttempts()[0].PostconditionSummary() != observerelation.PostconditionUnknown {
				t.Fatalf("state host route attempt = %#v, want observation %q and unknown postcondition", state.HostRouteAttempts()[0], test.wantObservation)
			}
		})
	}
}
