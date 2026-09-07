package apply

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type unusedHostRouteWorkingDirectoryBinding struct {
	closeCount int
}

func (*unusedHostRouteWorkingDirectoryBinding) Validate() error {
	panic("unused host-route binding must not be validated")
}

func (*unusedHostRouteWorkingDirectoryBinding) OpenDirectory() (*os.File, error) {
	panic("unused host-route binding must not be opened")
}

func (binding *unusedHostRouteWorkingDirectoryBinding) Close() error {
	binding.closeCount++
	return nil
}

func TestExecuteHostRouteAttemptReleasesBindingWhenEnvPreflightDoesNotAcquireIt(t *testing.T) {
	binding := &unusedHostRouteWorkingDirectoryBinding{}
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})

	attempt, err := executeHostRouteAttempt(
		context.Background(),
		executor,
		subprocess.CommandAttemptRequest{
			Command: "unused-host-route",
			EnvRefs: []subprocess.CommandEnvRef{{
				Name:       "MISSING_HOST_ROUTE_ENV",
				SourceName: "MISSING_HOST_ROUTE_ENV",
			}},
		},
		binding,
	)
	if err != nil {
		t.Fatalf("executeHostRouteAttempt returned release error: %v", err)
	}
	if attempt.Reason() != subprocess.CommandReasonMissingEnvRef {
		t.Fatalf("attempt reason = %q, want %q", attempt.Reason(), subprocess.CommandReasonMissingEnvRef)
	}
	if binding.closeCount != 1 {
		t.Fatalf("binding close count = %d, want 1", binding.closeCount)
	}
}

func TestExecuteWithOptionsRejectsBlockedClaudePluginCarrierActionBeforeExecution(t *testing.T) {
	action := applyExtensionDerivedClaudePluginCarrierAction(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			applyClaudePluginCarrierUnmanagedRow(t, "context7@market"),
		},
	})

	reconciliation, err := reconciliation.NewResult(reconciliation.ResultInput{
		Context:   reconciliation.ContextApply,
		Relations: []reconciliation.RelationAction{action},
	})
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	result, err := ExecuteWithOptions(context.Background(), preparedWriteForTestResult(CommandResult{
		Reconciliation: reconciliation,
	}), ExecuteOptions{})
	if !errors.Is(err, ErrRelationActionBlock) {
		t.Fatalf("ExecuteWithOptions error = %v, want ErrRelationActionBlock", err)
	}
	if len(result.DelegateAttempts) != 0 {
		t.Fatalf("delegate attempts = %#v, want none", result.DelegateAttempts)
	}
	if result.ExecutionAttempted {
		t.Fatal("blocked relation reported crossing the execution boundary")
	}
}

func TestExecuteWithOptionsRejectsBlockedClaudePluginCarrierBeforeDelegateExecutor(t *testing.T) {
	action := applyExtensionDerivedClaudePluginCarrierAction(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			applyClaudePluginCarrierUnmanagedRow(t, "context7@market"),
		},
	})
	called := false
	executor := delegate.NewExecutor(delegate.Options{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			called = true
			return subprocess.CommandResult{Stdout: "should not execute"}
		},
	})

	reconciliation, err := reconciliation.NewResult(reconciliation.ResultInput{
		Context:   reconciliation.ContextApply,
		Relations: []reconciliation.RelationAction{action},
	})
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	result, err := ExecuteWithOptions(context.Background(), preparedWriteForTestResult(CommandResult{
		Reconciliation: reconciliation,
	}), ExecuteOptions{DelegateExecutor: executor})
	if !errors.Is(err, ErrRelationActionBlock) {
		t.Fatalf("ExecuteWithOptions error = %v, want ErrRelationActionBlock", err)
	}
	if called {
		t.Fatalf("delegate executor was called for blocked Claude plugin carrier relation")
	}
	if len(result.DelegateAttempts) != 0 {
		t.Fatalf("delegate attempts = %#v, want none", result.DelegateAttempts)
	}
}

func preparedWriteForTestResult(result CommandResult) *PreparedWrite {
	return &PreparedWrite{
		CommandResult: cloneCommandResult(result),
		lifecycle: &preparedWriteLifecycle{
			state: preparedWriteReady,
			planned: commandPlan{
				result:     result,
				assessment: readiness.Assessment{Reconciliation: result.Reconciliation},
			},
		},
	}
}

func TestExecuteWithOptionsRunsAdmittedHostRouteAndPersistsObservedPresent(t *testing.T) {
	tests := []struct {
		name         string
		scope        target.Scope
		hostScopeArg string
		rowScope     observeclaudeplugin.HostScope
	}{
		{
			name:         "project",
			scope:        target.ScopeProject,
			hostScopeArg: "project",
			rowScope:     observeclaudeplugin.HostScopeProject,
		},
		{
			name:         "global",
			scope:        target.ScopeGlobal,
			hostScopeArg: "user",
			rowScope:     observeclaudeplugin.HostScopeUser,
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
			observer := func(ctx context.Context, command executehostroute.Command, pending []durablecarrier.PendingCarrierInstall, _ []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
				assertApplyPendingCarrierInstallRows(t, pending, locked.Locked.Subjects()[0])
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
			if len(planning.Reconciliation.Relations()) != 1 || !planning.Reconciliation.Relations()[0].InvokesHostRoute() {
				t.Fatalf("RelationActions = %#v, want one host-route action", planning.Reconciliation.Relations())
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
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedObservedPresent, "observed_present", true)
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "claude-code", string(test.scope), "claude-code.plugin-carrier.install", durableattempt.HostRouteResultAttemptedObservedPresent, "observed_present", true)
			if test.scope == target.ScopeProject {
				assertApplyProjectManagedCarrierClaim(t, state, locked.Locked.Subjects()[0])
			} else {
				assertApplyNoCarrierFact(t, state, locked.Locked.Subjects()[0].SubjectID())
				assertApplyGlobalManagedCarrierClaim(t, root, locked.Locked.Subjects()[0])
			}
			if len(result.DelegateAttempts) != 0 {
				t.Fatalf("DelegateAttempts = %#v, want none for carrier host route", result.DelegateAttempts)
			}
		})
	}
}

func TestExecuteWithOptionsPromotesInterruptedGlobalInstallFromFreshPresence(t *testing.T) {
	root, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(
		t,
		target.ScopeGlobal,
	)
	record := locked.Locked.Subjects()[0]
	statePath := filepath.Join(root, ".daem", "state.json")
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(statePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, statefileKey), manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	writeApplyStatefile(t, statePath, applyStateSnapshot(t, durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	}))

	present := applyClaudeObservationBatch(t, locked, subject, applyClaudePluginCarrierInventory(
		t,
		observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				applyClaudePluginCarrierManagedRowWithScope(
					t,
					"context7@market",
					string(subject.ExpectedRelation().ManagedInstanceKey()),
					observeclaudeplugin.HostScopeUser,
				),
			},
		},
	))
	planning, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &present,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	actions := planning.Reconciliation.Relations()
	if len(actions) != 1 || actions[0].Kind() != reconciliation.ActionNoOp {
		t.Fatalf("RelationActions = %#v, want exact managed no-op", actions)
	}
	calls := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			calls++
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 99}
		},
	})
	result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
		RelationObservations: &present,
		HostRouteExecutor:    executor,
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if calls != 0 || len(result.HostRouteAttempts) != 0 {
		t.Fatalf("interrupted promotion invoked host route: calls=%d attempts=%#v", calls, result.HostRouteAttempts)
	}
	state := loadApplyStatefile(t, statePath)
	assertApplyNoCarrierFact(t, state, record.SubjectID())
	assertApplyGlobalManagedCarrierClaim(t, root, record)
}

func TestInterruptedGlobalPromotionRejectsChangedRegistryBaseline(t *testing.T) {
	root, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(
		t,
		target.ScopeGlobal,
	)
	record := locked.Locked.Subjects()[0]
	statePath := filepath.Join(root, ".daem", "state.json")
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(statePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, statefileKey), manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeApplyStatefile(t, statePath, current)
	present := applyClaudeObservationBatch(t, locked, subject, applyClaudePluginCarrierInventory(
		t,
		observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				applyClaudePluginCarrierManagedRowWithScope(
					t,
					"context7@market",
					string(subject.ExpectedRelation().ManagedInstanceKey()),
					observeclaudeplugin.HostScopeUser,
				),
			},
		},
	))
	planning, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &present,
	})
	if err != nil {
		t.Fatal(err)
	}
	actions := planning.Reconciliation.Relations()
	if len(actions) != 1 {
		t.Fatalf("RelationActions = %#v, want one interrupted promotion", actions)
	}
	correlation, presentCorrelation := actions[0].Correlation()
	if !presentCorrelation {
		t.Fatal("interrupted promotion action has no current correlation")
	}
	registry := durablecarrier.EmptyGlobalCarrierClaims()
	paths := applyTestPaths(t, root)
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	concurrentClaim := newWorkflowFixture(t, target.ScopeGlobal).claim
	durableCurrent, err := store.Upsert(t.Context(), concurrentClaim)
	if err != nil {
		t.Fatal(err)
	}
	bound := &recordingBoundStatefileAuthority{entry: &rootedpath.EntryAuthority{}}
	authority, err := newStatefileEffectAuthorityFromReservation(
		statefileEffectPlan{validations: 1, fileCommits: 1},
		&recordingStatefileReservation{bound: bound},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authority.Close(); err != nil {
			t.Errorf("close statefile authority: %v", err)
		}
	})
	plan, err := prepareGlobalCarrierPromotionSettlementPlan(
		paths.CarrierClaimRegistryPath,
		current,
		registry,
		actions[0],
		correlation,
	)
	if err != nil {
		t.Fatal(err)
	}
	nextState, nextRegistry, err := commitObservedGlobalCarrierClaim(
		t.Context(),
		paths,
		authority,
		current,
		registry,
		actions[0],
		correlation,
		plan,
		testGlobalCarrierSettlementOptions(t, paths),
	)
	if err == nil || !strings.Contains(err.Error(), "changed since confirmed observation") {
		t.Fatalf("commitObservedGlobalCarrierClaim error = %v, want stale baseline rejection", err)
	}

	if len(nextState.PendingCarrierInstalls()) != 1 {
		t.Fatalf("pending installs = %#v, want original pending fact", nextState.PendingCarrierInstalls())
	}
	if !nextRegistry.Equal(registry) {
		t.Fatalf("returned registry = %#v, want confirmed baseline", nextRegistry.Claims())
	}
	actual, loadErr := store.Load(t.Context())
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !actual.Equal(durableCurrent) {
		t.Fatalf(
			"durable registry = %#v, want unchanged %#v",
			actual.Claims(),
			durableCurrent.Claims(),
		)
	}
}

func TestExecuteWithOptionsRunsAdmittedCodexHostRouteAndPersistsObservedPresent(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, locked, subject := writeApplyCodexPluginCarrierCommandFixture(t)
	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock: fixedApplyHostRouteClock,
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
	observer := func(ctx context.Context, command executehostroute.Command, pending []durablecarrier.PendingCarrierInstall, _ []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
		assertApplyPendingCarrierInstallRows(t, pending, locked.Locked.Subjects()[0])
		return assurancehostroute.CurrentObservation(observerelation.Correlate(subject.ExpectedRelation(), applyRelationInventory(t, observerelation.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observerelation.Row{
				applyRelationManagedRow(t, "documents@openai-primary-runtime", string(subject.ExpectedRelation().ManagedInstanceKey())),
			},
		})))
	}

	planning, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"codex"},
		RelationObservations: &missingInventory,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	if len(planning.Reconciliation.Relations()) != 1 ||
		planning.Reconciliation.Relations()[0].Kind() != reconciliation.ActionCreate ||
		!planning.Reconciliation.Relations()[0].InvokesHostRoute() {
		t.Fatalf("RelationActions = %#v, want one promotable host-route create", planning.Reconciliation.Relations())
	}
	result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
		RelationObservations: &missingInventory,
		HostRouteExecutor:    executor,
		HostRouteObserver:    observer,
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	assertApplyCodexHostRouteCommandRequest(t, root, requests)
	assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), "codex", "global", "codex.plugin-carrier.install", durableattempt.HostRouteResultAttemptedObservedPresent, "observed_present", true)
	state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
	assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), "codex", "global", "codex.plugin-carrier.install", durableattempt.HostRouteResultAttemptedObservedPresent, "observed_present", true)
	assertApplyNoCarrierFact(t, state, locked.Locked.Subjects()[0].SubjectID())
	assertApplyGlobalManagedCarrierClaim(t, root, locked.Locked.Subjects()[0])
	if len(result.DelegateAttempts) != 0 {
		t.Fatalf("DelegateAttempts = %#v, want none for carrier host route", result.DelegateAttempts)
	}
}

func TestExecuteWithOptionsPersistsAttemptWhenPostRouteObservationIsInvalid(t *testing.T) {
	tests := []struct {
		name        string
		exitCode    int
		resultClass durableattempt.HostRouteResultClass
		reason      durableattempt.HostRouteResultReason
	}{
		{
			name:        "successful command",
			resultClass: durableattempt.HostRouteResultAttemptedUnverified,
			reason:      durableattempt.HostRouteReasonObservationParseFailed,
		},
		{
			name:        "failed command",
			exitCode:    7,
			resultClass: durableattempt.HostRouteResultFailed,
			reason:      durableattempt.HostRouteReasonNonZeroExit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixture(t)
			var requests []subprocess.CommandRequest
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					requests = append(requests, request)
					return subprocess.CommandResult{
						Started:     true,
						HasExitCode: true,
						ExitCode:    test.exitCode,
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
				HostRouteObserver: func(context.Context, executehostroute.Command, []durablecarrier.PendingCarrierInstall, []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
					return assurancehostroute.CurrentObservation(observerelation.CorrelationResult{})
				},
			})
			if err == nil ||
				!strings.Contains(err.Error(), string(assurancehostroute.ResultReasonUnsupportedObservation)) ||
				!strings.Contains(err.Error(), "host route attempt failed") {
				t.Fatalf("ExecuteWithOptions error = %v, want post-attempt observation classification failure", err)
			}
			assertApplyClaudeHostRouteCommandRequest(t, root, requests, "project")
			assertApplyHostRouteAttemptFor(
				t,
				result.HostRouteAttempts,
				locked.Locked.Subjects()[0].SubjectID(),
				"claude-code",
				"project",
				"claude-code.plugin-carrier.install",
				test.resultClass,
				test.reason,
				false,
			)
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(
				t,
				state.HostRouteAttempts(),
				locked.Locked.Subjects()[0].SubjectID(),
				"claude-code",
				"project",
				"claude-code.plugin-carrier.install",
				test.resultClass,
				test.reason,
				false,
			)
			assertApplyNoCarrierFact(t, state, locked.Locked.Subjects()[0].SubjectID())
		})
	}
}

func TestExecuteWithOptionsReportsIndeterminateRouteWorkDirWithoutWritingReplacementRoot(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyCodexPluginCarrierCommandFixture(t)
	moved := root + "-captured"
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	runnerCalled := false
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock: fixedApplyHostRouteClock,
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			runnerCalled = true
			if err := os.Rename(root, moved); err != nil {
				return subprocess.CommandResult{Started: true, Err: err}
			}
			if err := os.Mkdir(root, 0o700); err != nil {
				return subprocess.CommandResult{Started: true, Err: err}
			}
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
		HostRouteObserver: func(context.Context, executehostroute.Command, []durablecarrier.PendingCarrierInstall, []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
			return assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationUnavailable)
		},
	})
	if !runnerCalled {
		t.Fatal("host route runner was not called")
	}
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ExecuteWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	assertApplyHostRouteAttemptFor(
		t,
		result.HostRouteAttempts,
		locked.Locked.Subjects()[0].SubjectID(),
		"codex",
		"global",
		"codex.plugin-carrier.install",
		durableattempt.HostRouteResultFailed,
		durableattempt.HostRouteReasonWorkDirAuthority,
		false,
	)
	if _, statErr := os.Stat(filepath.Join(root, ".daem", "state.json")); !os.IsNotExist(statErr) {
		t.Fatalf("replacement-root statefile stat error = %v, want absent", statErr)
	}
	capturedStatePath := filepath.Join(moved, ".daem", "state.json")
	content, readErr := os.ReadFile(capturedStatePath)
	if readErr != nil {
		t.Fatalf("read captured-root write-ahead state: %v", readErr)
	}
	if !bytes.Contains(content, []byte(`"pending_carrier_installs"`)) ||
		!bytes.Contains(content, []byte(locked.Locked.Subjects()[0].SubjectID().Key())) {
		t.Fatalf("captured-root state = %s, want exact pending Codex install", content)
	}
}

func TestRunHostRoutesPersistsAndRetiresActionAttemptPendingInstall(t *testing.T) {
	_, manifestPath, lockfilePath, missingInventory, _, _ := writeApplyAntigravityCLIPluginCarrierCommandFixture(t)
	planning, err := PlanDryRun(t.Context(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"antigravity-cli"},
		RelationObservations: &missingInventory,
	})
	if err != nil {
		t.Fatalf("PlanDryRun returned error: %v", err)
	}
	seed := planning.Reconciliation.Relations()
	if len(seed) != 1 {
		t.Fatalf("RelationActions = %#v, want one seed action", seed)
	}
	admission, err := reconciliation.NewRelationRouteAdmissionDecision(
		reconciliation.RelationRouteAdmissionSpec{
			Row:               reconciliation.RouteAdmissionRowInstallCarrier,
			RequestedOutcome:  reconciliation.AdmissionOutcomeOrdinaryMutation,
			SelectedOutcome:   reconciliation.AdmissionOutcomeHostDelegated,
			ObservationPolicy: reconciliation.ObservationAttemptWhenUnsupported,
		},
	)
	if err != nil {
		t.Fatalf("NewRelationRouteAdmissionDecision returned error: %v", err)
	}
	action, err := reconciliation.NewRelationAction(reconciliation.RelationActionInput{
		CarrierIdentity: seed[0].CarrierIdentity(),
		RouteRequest:    seed[0].RouteRequest(),
		Correlation: observerelation.Correlate(
			seed[0].ExpectedRelation(),
			observerelation.UnsupportedInventory(),
		),
		RouteAdmission: admission,
	})
	if err != nil {
		t.Fatalf("NewRelationAction returned error: %v", err)
	}
	if action.Kind() != reconciliation.ActionAttempt || !action.InvokesHostRoute() {
		t.Fatalf("RelationAction = %#v, want invoking ActionAttempt", action)
	}

	paths := planning.planned.context.Paths
	pendingObserved := false
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock: fixedApplyHostRouteClock,
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			state := loadApplyStatefile(t, paths.StatefilePath)
			pending := state.PendingCarrierInstalls()
			pendingObserved = len(pending) == 1 &&
				pending[0].Identity().ExactEqual(action.CarrierIdentity()) &&
				pending[0].InstallRequest().Equal(action.RouteRequest())
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
	options := applyDelegateRunOptions(t, paths, runOptions{HostRouteExecutor: executor})
	current := planning.planned.assessment.CurrentState
	next, _, records, err := runHostRoutesAndPersistAttemptRecords(
		t.Context(),
		paths,
		planning.planned.context.Lockfile,
		planning.planned.assessment.StatePath,
		current,
		planning.planned.assessment.Owner,
		planning.planned.assessment.GlobalCarrierClaims,
		[]reconciliation.RelationAction{action},
		options,
	)
	if err != nil {
		t.Fatalf("runHostRoutesAndPersistAttemptRecords returned error: %v", err)
	}
	if !pendingObserved {
		t.Fatal("host invocation did not observe its exact pending-install write-ahead")
	}
	if len(records) != 1 {
		t.Fatalf("host route records = %#v, want one ActionAttempt record", records)
	}
	if pending := next.PendingCarrierInstalls(); len(pending) != 0 {
		t.Fatalf("returned pending installs = %#v, want ordinary completion retired", pending)
	}
	if pending := loadApplyStatefile(t, paths.StatefilePath).PendingCarrierInstalls(); len(pending) != 0 {
		t.Fatalf("persisted pending installs = %#v, want ordinary completion retired", pending)
	}
}

func TestExecuteWithOptionsRunsAdmittedHostSourceRoutesAndPersistsAttemptedUnverified(t *testing.T) {
	tests := []struct {
		name          string
		targetValue   string
		fixture       func(*testing.T) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation)
		assertRequest func(*testing.T, string, []subprocess.CommandRequest)
		wantScope     string
		wantRouteID   string
	}{
		{
			name:        "opencode project plugin",
			targetValue: "opencode",
			fixture: func(t *testing.T) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
				return writeApplyOpenCodePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
			},
			assertRequest: func(t *testing.T, root string, requests []subprocess.CommandRequest) {
				assertApplyOpenCodeHostRouteCommandRequestForScope(t, root, requests, target.ScopeProject)
			},
			wantScope:   "project",
			wantRouteID: "opencode.plugin-carrier.install",
		},
		{
			name:          "opencode global plugin",
			targetValue:   "opencode",
			fixture:       writeApplyOpenCodePluginCarrierCommandFixture,
			assertRequest: assertApplyOpenCodeHostRouteCommandRequest,
			wantScope:     "global",
			wantRouteID:   "opencode.plugin-carrier.install",
		},
		{
			name:          "pi project package",
			targetValue:   "pi",
			fixture:       writeApplyPiPackageCarrierCommandFixture,
			assertRequest: assertApplyPiHostRouteCommandRequest,
			wantScope:     "project",
			wantRouteID:   "pi.package-carrier.install",
		},
		{
			name:        "pi global package",
			targetValue: "pi",
			fixture: func(t *testing.T) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
				return writeApplyPiPackageCarrierCommandFixtureForScope(t, target.ScopeGlobal)
			},
			assertRequest: func(t *testing.T, root string, requests []subprocess.CommandRequest) {
				assertApplyPiHostRouteCommandRequestForScope(t, root, requests, target.ScopeGlobal)
			},
			wantScope:   "global",
			wantRouteID: "pi.package-carrier.install",
		},
		{
			name:          "antigravity cli global plugin",
			targetValue:   "antigravity-cli",
			fixture:       writeApplyAntigravityCLIPluginCarrierCommandFixture,
			assertRequest: assertApplyAntigravityCLIHostRouteCommandRequest,
			wantScope:     "global",
			wantRouteID:   "antigravity-cli.plugin-carrier.install",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, _ := test.fixture(t)
			var requests []subprocess.CommandRequest
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					requests = append(requests, request)
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
			if len(planning.Reconciliation.Relations()) != 1 || !planning.Reconciliation.Relations()[0].InvokesHostRoute() {
				t.Fatalf("RelationActions = %#v, want one host-route action", planning.Reconciliation.Relations())
			}
			result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
				RelationObservations: &missingInventory,
				HostRouteExecutor:    executor,
			})
			if err != nil {
				t.Fatalf("ExecuteWithOptions returned error: %v", err)
			}
			test.assertRequest(t, root, requests)
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), test.targetValue, test.wantScope, test.wantRouteID, durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), test.targetValue, test.wantScope, test.wantRouteID, durableattempt.HostRouteResultAttemptedUnverified, "observation_unavailable", false)
		})
	}
}

func TestExecuteWithOptionsRecordsHostSourceRouteFailures(t *testing.T) {
	tests := []struct {
		name         string
		targetValue  string
		fixture      func(*testing.T) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation)
		rawResult    subprocess.CommandResult
		wantScope    string
		wantRouteID  string
		wantReason   durableattempt.HostRouteResultReason
		wantExitCode *int
	}{
		{
			name:        "opencode missing executable",
			targetValue: "opencode",
			fixture:     writeApplyOpenCodePluginCarrierCommandFixture,
			rawResult: subprocess.CommandResult{
				MissingRunner: true,
				Err:           errors.New("opencode executable not found"),
			},
			wantScope:   "global",
			wantRouteID: "opencode.plugin-carrier.install",
			wantReason:  durableattempt.HostRouteReasonMissingRunner,
		},
		{
			name:        "pi nonzero exit",
			targetValue: "pi",
			fixture:     writeApplyPiPackageCarrierCommandFixture,
			rawResult: subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    17,
			},
			wantScope:    "project",
			wantRouteID:  "pi.package-carrier.install",
			wantReason:   durableattempt.HostRouteReasonNonZeroExit,
			wantExitCode: intPtr(17),
		},
		{
			name:        "antigravity missing executable",
			targetValue: "antigravity-cli",
			fixture:     writeApplyAntigravityCLIPluginCarrierCommandFixture,
			rawResult: subprocess.CommandResult{
				MissingRunner: true,
				Err:           errors.New("agy executable not found"),
			},
			wantScope:   "global",
			wantRouteID: "antigravity-cli.plugin-carrier.install",
			wantReason:  durableattempt.HostRouteReasonMissingRunner,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, _ := test.fixture(t)
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Clock: fixedApplyHostRouteClock,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					return test.rawResult
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
			if err == nil || !strings.Contains(err.Error(), "host route attempt failed") {
				t.Fatalf("ExecuteWithOptions error = %v, want host route failure", err)
			}
			assertApplyHostRouteAttemptFor(t, result.HostRouteAttempts, locked.Locked.Subjects()[0].SubjectID(), test.targetValue, test.wantScope, test.wantRouteID, durableattempt.HostRouteResultFailed, test.wantReason, false)
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			assertApplyHostRouteAttemptFor(t, state.HostRouteAttempts(), locked.Locked.Subjects()[0].SubjectID(), test.targetValue, test.wantScope, test.wantRouteID, durableattempt.HostRouteResultFailed, test.wantReason, false)
			exitCode, hasExitCode := state.HostRouteAttempts()[0].ExitCode()
			if test.wantExitCode == nil {
				if hasExitCode {
					t.Fatalf("exit code = %d, want absent", exitCode)
				}
			} else if !hasExitCode || exitCode != *test.wantExitCode {
				t.Fatalf("exit code = %d present=%t, want %d", exitCode, hasExitCode, *test.wantExitCode)
			}
		})
	}
}

func TestPlanWriteBlocksObservedPresentRelationWithoutManagementAuthority(t *testing.T) {
	_, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixture(t)
	presentInventory := applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			applyClaudePluginCarrierManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey())),
		},
	})
	presentObservations := applyClaudeObservationBatch(t, locked, subject, presentInventory)

	_, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &presentObservations,
	})
	if !errors.Is(err, ErrRelationActionBlock) ||
		!strings.Contains(err.Error(), "reason=present_unclaimed") {
		t.Fatalf("PlanWrite error = %v, want present-unclaimed relation block", err)
	}
}
