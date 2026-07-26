package refresh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	lockworkflow "github.com/isty2e/daem/internal/workflow/lock"
)

func TestSyntheticNoObserverRefreshDryRunAndExecute(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	manifestBefore := readTestFile(t, manifestPath)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	lockBefore := readTestFile(t, paths.LockfilePath)
	options := PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)}

	dryRun, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, options)
	if err != nil {
		t.Fatalf("PlanDryRun returned error: %v", err)
	}
	if dryRun.Mode != ModeDryRun || dryRun.ResultClass != ResultPlanned {
		t.Fatalf("dry-run mode/class = %q/%q", dryRun.Mode, dryRun.ResultClass)
	}
	if dryRun.Route.Operation != lock.OperationRefresh ||
		dryRun.Route.ObservationPosture != PostureAttemptWhenUnsupported {
		t.Fatalf("dry-run route = %#v", dryRun.Route)
	}

	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, options)
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	disclosure := prepared.Disclosure()
	if disclosure.Disclosure.Invocation.Command != "synthetic-host" ||
		!slices.Equal(disclosure.Disclosure.Invocation.Args, []string{"refresh", "formatter"}) {
		t.Fatalf("disclosure invocation = %#v", disclosure.Disclosure.Invocation)
	}

	var requests []subprocess.CommandRequest
	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Clock: func() time.Time {
				return time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
			},
			Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				requests = append(requests, request)
				return subprocess.CommandResult{
					Started:     true,
					Stdout:      "token=claude-refresh-secret",
					Stderr:      "{malformed-host-output",
					ExitCode:    0,
					HasExitCode: true,
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(requests) != 1 ||
		requests[0].Command != "synthetic-host" ||
		!slices.Equal(requests[0].Args, []string{"refresh", "formatter"}) {
		t.Fatalf("requests = %#v", requests)
	}
	if result.ResultClass != ResultAttemptedUnverified ||
		!result.Attempted ||
		!result.AttemptHistory.Persisted {
		t.Fatalf("result = %#v", result)
	}
	snapshot, err := statefile.LoadOptional(context.Background(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("LoadOptional returned error: %v", err)
	}
	attempts := snapshot.HostRouteAttempts()
	if len(attempts) != 1 ||
		attempts[0].Operation() != lock.OperationRefresh ||
		attempts[0].RouteRequestHash() != result.Route.RequestHash {
		t.Fatalf("attempts = %#v", attempts)
	}
	if got := readTestFile(t, manifestPath); !slices.Equal(got, manifestBefore) {
		t.Fatal("refresh changed manifest bytes")
	}
	if got := readTestFile(t, paths.LockfilePath); !slices.Equal(got, lockBefore) {
		t.Fatal("refresh changed lockfile bytes")
	}
	if _, secondErr := Execute(context.Background(), prepared, ExecuteOptions{}); !errors.Is(
		secondErr,
		ErrPreparedCommandConsumed,
	) {
		t.Fatalf("second Execute error = %v", secondErr)
	}
}

func TestSyntheticRefreshRejectsManifestDriftBeforeLaunch(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	file, err := os.OpenFile(manifestPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile returned error: %v", err)
	}
	if _, err := file.WriteString("\n# changed after disclosure\n"); err != nil {
		t.Fatalf("WriteString returned error: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	called := false
	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				called = true
				return subprocess.CommandResult{Started: true}
			},
		},
	})
	if err == nil {
		t.Fatal("Execute returned nil error")
	}
	if called {
		t.Fatal("host runner was called after manifest drift")
	}
	if result.ResultClass != ResultRefused || result.ReasonCode != ReasonStalePlan {
		t.Fatalf("result = %#v", result)
	}
}

func TestSyntheticRefreshRecordsStartedFailure(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				return subprocess.CommandResult{
					Started:     true,
					ExitCode:    17,
					HasExitCode: true,
				}
			},
		},
	})
	if err == nil {
		t.Fatal("Execute returned nil error")
	}
	if result.ResultClass != ResultFailed ||
		!result.Attempted ||
		!result.AttemptHistory.Persisted {
		t.Fatalf("result = %#v", result)
	}
	snapshot, err := statefile.LoadOptional(context.Background(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("LoadOptional returned error: %v", err)
	}
	attempts := snapshot.HostRouteAttempts()
	if len(attempts) != 1 ||
		attempts[0].AttemptReason() != "nonzero_exit" {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestSyntheticRefreshDoesNotRecordUnstartedAttempt(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				return subprocess.CommandResult{MissingRunner: true}
			},
		},
	})
	if err == nil {
		t.Fatal("Execute returned nil error")
	}
	if result.ResultClass != ResultFailed ||
		result.Attempted ||
		result.AttemptHistory.Persisted ||
		result.ProcessOutcome == nil ||
		result.ProcessOutcome.Reason != subprocess.CommandReasonMissingRunner {
		t.Fatalf("result = %#v", result)
	}
	snapshot, err := statefile.LoadOptional(context.Background(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("LoadOptional returned error: %v", err)
	}
	if attempts := snapshot.HostRouteAttempts(); len(attempts) != 0 {
		t.Fatalf("attempts = %#v, want none", attempts)
	}
}

func TestSyntheticRefreshPersistenceFailureIsPartialWithoutRetry(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	calls := 0
	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				if err := os.MkdirAll(paths.StatefilePath, 0o700); err != nil {
					t.Fatalf("MkdirAll statefile path: %v", err)
				}
				return subprocess.CommandResult{
					Started:     true,
					ExitCode:    0,
					HasExitCode: true,
				}
			},
		},
	})
	if err == nil {
		t.Fatal("Execute returned nil error")
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1", calls)
	}
	if result.ResultClass != ResultPartial ||
		result.ReasonCode != ReasonAttemptPersistence ||
		result.AttemptHistory.Persisted {
		t.Fatalf("result = %#v", result)
	}
}

func TestSyntheticObservedRefreshRequiresExactPreAndPostObservation(t *testing.T) {
	manifestPath, inventoryPath := writeObservedRefreshFixture(t)
	observer := syntheticObserver(
		t,
		inventoryPath,
		observerelation.StateExactCorrelation,
		observerelation.StateExactCorrelation,
		observerelation.StateExactCorrelation,
		observerelation.StateExactCorrelation,
	)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "context7",
	}, PlanOptions{
		CommandBuilder: syntheticRefreshCommandBuilder(t),
		Observer:       observer,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				return subprocess.CommandResult{
					Started:     true,
					ExitCode:    0,
					HasExitCode: true,
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.ResultClass != ResultObservedRelation ||
		result.Observation == nil ||
		result.Observation.State != observerelation.StateExactCorrelation ||
		!result.AttemptHistory.Persisted {
		t.Fatalf("result = %#v", result)
	}
}

func TestSyntheticObservedRefreshRefusesMissingPreObservation(t *testing.T) {
	manifestPath, inventoryPath := writeObservedRefreshFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "context7",
	}, PlanOptions{
		CommandBuilder: syntheticRefreshCommandBuilder(t),
		Observer: syntheticObserver(
			t,
			inventoryPath,
			observerelation.StateMissing,
		),
	})
	if err == nil {
		t.Fatal("PlanWrite returned nil error")
	}
	t.Cleanup(func() { _ = prepared.Close() })
	result := prepared.Disclosure()
	if result.ResultClass != ResultRefused ||
		result.ReasonCode != ReasonRelationMissing {
		t.Fatalf("result = %#v", result)
	}
}

func TestSyntheticObservedRefreshMakesMissingPostObservationPartial(t *testing.T) {
	manifestPath, inventoryPath := writeObservedRefreshFixture(t)
	observer := syntheticObserver(
		t,
		inventoryPath,
		observerelation.StateExactCorrelation,
		observerelation.StateExactCorrelation,
		observerelation.StateExactCorrelation,
		observerelation.StateMissing,
	)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "context7",
	}, PlanOptions{
		CommandBuilder: syntheticRefreshCommandBuilder(t),
		Observer:       observer,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				return subprocess.CommandResult{
					Started:     true,
					ExitCode:    0,
					HasExitCode: true,
				}
			},
		},
	})
	if err == nil {
		t.Fatal("Execute returned nil error")
	}
	if result.ResultClass != ResultPartial ||
		result.ReasonCode != ReasonPostObservationFailed ||
		result.Observation == nil ||
		result.Observation.State != observerelation.StateMissing ||
		!result.AttemptHistory.Persisted {
		t.Fatalf("result = %#v", result)
	}
}

func syntheticRefreshCommandBuilder(t *testing.T) CommandBuilder {
	t.Helper()
	disclosure, err := executehostroute.NewDisclosure(executehostroute.DisclosureInput{
		ExecutionSubject:      "synthetic package formatter",
		InvocationKind:        executehostroute.InvocationKindCommand,
		CWDPolicy:             executehostroute.CWDPolicySelectedRoot,
		EffectClasses:         []string{"host_package_refresh"},
		RetainedEffectClasses: []string{"host_cache"},
		NonClaims:             []string{"no_rollback"},
	})
	if err != nil {
		t.Fatalf("NewDisclosure returned error: %v", err)
	}
	return func(input CommandBuildInput) (CommandSpec, error) {
		if input.Operation != lock.OperationRefresh {
			return CommandSpec{}, errors.New("synthetic adapter received non-refresh operation")
		}
		return NewCommandSpec(subprocess.CommandAttemptRequest{
			Command: "synthetic-host",
			Args:    []string{"refresh", "formatter"},
			WorkDir: input.WorkDir,
		}, disclosure)
	}
}

func syntheticObserver(
	t *testing.T,
	inventoryPath string,
	states ...observerelation.CorrelationState,
) RelationObserver {
	t.Helper()
	call := 0
	return func(
		_ context.Context,
		input ObservationRequest,
	) (RelationObservation, error) {
		if call >= len(states) {
			return RelationObservation{}, errors.New("unexpected observer call")
		}
		state := states[call]
		call++
		contract, found := input.Lockfile.Locked.Subject(input.Subject)
		if !found {
			return RelationObservation{}, errors.New("selected subject missing from lock")
		}
		realization, ok := contract.Realization()
		if !ok {
			return RelationObservation{}, errors.New("selected subject has no realization")
		}
		delegated, ok := realization.DelegatedRelation()
		if !ok {
			return RelationObservation{}, errors.New("selected subject is not delegated")
		}
		var rows []observerelation.Row
		switch state {
		case observerelation.StateExactCorrelation:
			expected := delegated.ExpectedRelation()
			row, err := observerelation.NewRow(observerelation.RowSpec{
				SubjectKey:            string(expected.SubjectKey()),
				HasManagedInstanceKey: true,
				ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
			})
			if err != nil {
				return RelationObservation{}, err
			}
			rows = []observerelation.Row{row}
		case observerelation.StateUnkeyedSameSubject:
			row, err := observerelation.NewRow(observerelation.RowSpec{
				SubjectKey: string(
					delegated.ExpectedRelation().SubjectKey(),
				),
			})
			if err != nil {
				return RelationObservation{}, err
			}
			rows = []observerelation.Row{row}
		case observerelation.StateMissing:
		default:
			return RelationObservation{}, errors.New("unsupported synthetic observation state")
		}
		inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows:         rows,
		})
		if err != nil {
			return RelationObservation{}, err
		}
		authorityPath, err := observerelation.NewAuthorityPath(
			inventoryPath,
			input.Target,
			input.Scope,
		)
		if err != nil {
			return RelationObservation{}, err
		}
		return RelationObservation{
			Result: observerelation.Correlate(
				delegated.ExpectedRelation(),
				inventory,
			),
			Present:        true,
			AuthorityPaths: []observerelation.AuthorityPath{authorityPath},
		}, nil
	}
}

func writeNoObserverRefreshFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	content := []byte(`version = 1
targets = ["opencode"]

[[extension]]
id = "formatter"
carrier = "opencode-plugin"
scope = "project"
source = { host_source = "@acme/formatter" }
`)
	if err := os.WriteFile(manifestPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if _, err := lockworkflow.RunLock(context.Background(), lockworkflow.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	return manifestPath
}

func writeObservedRefreshFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	inventoryPath := filepath.Join(root, "claude-inventory.json")
	content := []byte(`version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
scope = "project"
source = { marketplace = "context7@market" }
`)
	if err := os.WriteFile(manifestPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(inventoryPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile inventory: %v", err)
	}
	if _, err := lockworkflow.RunLock(context.Background(), lockworkflow.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	return manifestPath, inventoryPath
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q: %v", path, err)
	}
	return content
}
