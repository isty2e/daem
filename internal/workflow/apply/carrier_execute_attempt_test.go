package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestFinalAttemptConstructionFailureRetiresCompletedPendingCarrierInstall(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &missingInventory,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	var calls int
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock: func() time.Time { return time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			calls++
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	_, err = ExecuteWithOptions(context.Background(), planned, ExecuteOptions{
		RelationObservations: &missingInventory,
		HostRouteExecutor:    executor,
	})
	if err == nil || !strings.Contains(err.Error(), "host route attempt observed time is outside the durable RFC3339Nano range") {
		t.Fatalf("ExecuteWithOptions error = %v, want final attempt construction failure", err)
	}
	if calls != 1 {
		t.Fatalf("host route calls = %d, want one successful mechanical attempt", calls)
	}
	statePath := filepath.Join(root, ".daem", "state.json")
	state := loadApplyStatefile(t, statePath)
	assertApplyNoCarrierFact(t, state, locked.Locked.Subjects()[0].SubjectID())
	if len(state.HostRouteAttempts()) != 0 {
		t.Fatalf("HostRouteAttempts = %#v, want no falsely persisted final attempt", state.HostRouteAttempts())
	}
}

func TestExecuteWithOptionsFailsAndPersistsClaudeHostRouteAttemptOnCommandFailure(t *testing.T) {
	tests := []struct {
		name         string
		scope        target.Scope
		rawResult    subprocess.CommandResult
		wantReason   durableattempt.HostRouteResultReason
		wantExitCode *int
	}{
		{
			name:  "project missing runner",
			scope: target.ScopeProject,
			rawResult: subprocess.CommandResult{
				MissingRunner: true,
				Err:           errors.New("claude executable not found"),
			},
			wantReason: durableattempt.HostRouteReasonMissingRunner,
		},
		{
			name:  "global missing runner",
			scope: target.ScopeGlobal,
			rawResult: subprocess.CommandResult{
				MissingRunner: true,
				Err:           errors.New("claude executable not found"),
			},
			wantReason: durableattempt.HostRouteReasonMissingRunner,
		},
		{
			name:  "project nonzero exit",
			scope: target.ScopeProject,
			rawResult: subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    17,
			},
			wantReason:   durableattempt.HostRouteReasonNonZeroExit,
			wantExitCode: intPtr(17),
		},
		{
			name:  "global nonzero exit",
			scope: target.ScopeGlobal,
			rawResult: subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    17,
			},
			wantReason:   durableattempt.HostRouteReasonNonZeroExit,
			wantExitCode: intPtr(17),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixtureForScope(t, test.scope)
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					return test.rawResult
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
			if err == nil {
				t.Fatal("ExecuteWithOptions returned nil error for failed host route command")
			}
			if !strings.Contains(err.Error(), "host route attempt failed") ||
				!strings.Contains(err.Error(), string(test.wantReason)) {
				t.Fatalf("ExecuteWithOptions error = %v, want host route failure reason %q", err, test.wantReason)
			}
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultFailed, test.wantReason, false)
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultFailed, test.wantReason, false)
			assertApplyNoCarrierFact(t, state, locked.Locked.Subjects()[0].SubjectID())
			exitCode, hasExitCode := state.HostRouteAttempts()[0].ExitCode()
			if test.wantExitCode == nil {
				if hasExitCode {
					t.Fatalf("exit code = %d, want absent", exitCode)
				}
				return
			}
			if !hasExitCode || exitCode != *test.wantExitCode {
				t.Fatalf("exit code = %d present=%t, want %d", exitCode, hasExitCode, *test.wantExitCode)
			}
		})
	}
}

func TestExecuteWithOptionsDoesNotPersistClaudeHostRouteFailureOutput(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeGlobal)
	longOutput := strings.Repeat(" api_key=literal-token", 200)
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock:       fixedApplyHostRouteClock,
		OutputLimit: 24,
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    17,
				Stdout:      "token=super-secret" + longOutput,
				Stderr:      `password=hunter2 shell="$(touch should-not-run)"`,
				Err:         errors.New("runner secret=super-secret"),
			}
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
	if err == nil {
		t.Fatal("ExecuteWithOptions returned nil error for failed host route command")
	}
	for _, forbidden := range []string{"super-secret", "literal-token", "hunter2", "should-not-run"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("ExecuteWithOptions error leaked %q: %v", forbidden, err)
		}
	}
	assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "claude-code", "global", "claude-code.plugin-carrier.install", durableattempt.HostRouteResultFailed, durableattempt.HostRouteReasonNonZeroExit, false)
	if !result.HostRouteAttempts[0].Redacted() {
		t.Fatalf("result host route attempt = %#v, want redacted marker", result.HostRouteAttempts[0])
	}
	statePath := filepath.Join(root, ".daem", "state.json")
	state := loadApplyStatefile(t, statePath)
	assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "claude-code", "global", "claude-code.plugin-carrier.install", durableattempt.HostRouteResultFailed, durableattempt.HostRouteReasonNonZeroExit, false)
	if !state.HostRouteAttempts()[0].Redacted() {
		t.Fatalf("state host route attempt = %#v, want redacted marker", state.HostRouteAttempts()[0])
	}
	content, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", statePath, err)
	}
	for _, forbidden := range []string{
		"super-secret",
		"literal-token",
		"hunter2",
		"should-not-run",
		"token=",
		"api_key=",
		"password=",
	} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("statefile leaked %q: %s", forbidden, content)
		}
	}
}

func TestExecuteWithOptionsRerunsHostRouteWhenPriorAttemptHistoryIsStale(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixture(t)
	prior := applyPriorHostRouteAttemptRecord(t, locked.Locked.Subjects()[0])
	writeApplyStatefile(t, filepath.Join(root, ".daem", "state.json"), applyStateSnapshot(t, durable.SnapshotInput{
		HostRouteAttempts: []durableattempt.HostRouteAttempt{prior},
	}))
	calls := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock: fixedApplyHostRouteClock,
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			calls++
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
	if calls != 1 {
		t.Fatalf("host route calls = %d, want rerun despite prior history", calls)
	}
	assertApplyHostRouteAttempt(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
	state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
	assertApplyHostRouteAttempt(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
	if state.HostRouteAttempts()[0].ObservedAt().Equal(prior.ObservedAt()) {
		t.Fatalf("host route attempt history was not replaced: %#v", state.HostRouteAttempts()[0])
	}
}

func TestExecuteWithOptionsRerunsClaudeHostRouteAfterPriorAttemptedUnverified(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
			prior := applyPriorAttemptedUnverifiedHostRouteAttemptRecord(t, locked.Locked.Subjects()[0])
			writeApplyStatefile(t, filepath.Join(root, ".daem", "state.json"), applyStateSnapshot(t, durable.SnapshotInput{
				HostRouteAttempts: []durableattempt.HostRouteAttempt{prior},
			}))
			calls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					calls++
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
			if calls != 1 {
				t.Fatalf("host route calls = %d, want rerun despite prior attempted-unverified history", calls)
			}
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
			if state.HostRouteAttempts()[0].ObservedAt().Equal(prior.ObservedAt()) {
				t.Fatalf("prior attempted-unverified history was not replaced: %#v", state.HostRouteAttempts()[0])
			}
		})
	}
}

func TestExecuteWithOptionsRerunsCodexHostRouteWhenPriorAttemptHistoryIsStale(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyCodexPluginCarrierCommandFixture(t)
	prior := applyPriorHostRouteAttemptRecord(t, locked.Locked.Subjects()[0])
	writeApplyStatefile(t, filepath.Join(root, ".daem", "state.json"), applyStateSnapshot(t, durable.SnapshotInput{
		HostRouteAttempts: []durableattempt.HostRouteAttempt{prior},
	}))
	calls := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock: fixedApplyHostRouteClock,
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			calls++
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	planning, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"codex"},
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
	if calls != 1 {
		t.Fatalf("host route calls = %d, want rerun despite prior history", calls)
	}
	assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "codex", "global", "codex.plugin-carrier.install", durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
	state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
	assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "codex", "global", "codex.plugin-carrier.install", durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
	if state.HostRouteAttempts()[0].ObservedAt().Equal(prior.ObservedAt()) {
		t.Fatalf("host route attempt history was not replaced: %#v", state.HostRouteAttempts()[0])
	}
}

func TestExecuteWithOptionsRerunsHostSourceRoutesWhenPriorAttemptHistoryIsStale(t *testing.T) {
	tests := []struct {
		name        string
		targetValue string
		fixture     func(*testing.T) (string, string, string, relationobserve.Batch, lock.File, realization.DelegatedRelation)
		wantScope   string
		wantRouteID string
	}{
		{
			name:        "opencode project",
			targetValue: "opencode",
			fixture: func(t *testing.T) (string, string, string, relationobserve.Batch, lock.File, realization.DelegatedRelation) {
				return writeApplyOpenCodePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
			},
			wantScope:   "project",
			wantRouteID: "opencode.plugin-carrier.install",
		},
		{
			name:        "opencode global",
			targetValue: "opencode",
			fixture: func(t *testing.T) (string, string, string, relationobserve.Batch, lock.File, realization.DelegatedRelation) {
				return writeApplyOpenCodePluginCarrierCommandFixtureForScope(t, target.ScopeGlobal)
			},
			wantScope:   "global",
			wantRouteID: "opencode.plugin-carrier.install",
		},
		{
			name:        "pi project",
			targetValue: "pi",
			fixture: func(t *testing.T) (string, string, string, relationobserve.Batch, lock.File, realization.DelegatedRelation) {
				return writeApplyPiPackageCarrierCommandFixtureForScope(t, target.ScopeProject)
			},
			wantScope:   "project",
			wantRouteID: "pi.package-carrier.install",
		},
		{
			name:        "pi global",
			targetValue: "pi",
			fixture: func(t *testing.T) (string, string, string, relationobserve.Batch, lock.File, realization.DelegatedRelation) {
				return writeApplyPiPackageCarrierCommandFixtureForScope(t, target.ScopeGlobal)
			},
			wantScope:   "global",
			wantRouteID: "pi.package-carrier.install",
		},
		{
			name:        "antigravity-cli",
			targetValue: "antigravity-cli",
			fixture:     writeApplyAntigravityCLIPluginCarrierCommandFixture,
			wantScope:   "global",
			wantRouteID: "antigravity-cli.plugin-carrier.install",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, _ := test.fixture(t)
			prior := applyPriorHostRouteAttemptRecord(t, locked.Locked.Subjects()[0])
			writeApplyStatefile(t, filepath.Join(root, ".daem", "state.json"), applyStateSnapshot(t, durable.SnapshotInput{
				HostRouteAttempts: []durableattempt.HostRouteAttempt{prior},
			}))
			calls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					calls++
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			})

			planning, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{test.targetValue},
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
			if calls != 1 {
				t.Fatalf("host route calls = %d, want rerun despite prior history", calls)
			}
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), test.targetValue, test.wantScope, test.wantRouteID, durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), test.targetValue, test.wantScope, test.wantRouteID, durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
			if state.HostRouteAttempts()[0].ObservedAt().Equal(prior.ObservedAt()) {
				t.Fatalf("host route attempt history was not replaced: %#v", state.HostRouteAttempts()[0])
			}
		})
	}
}
