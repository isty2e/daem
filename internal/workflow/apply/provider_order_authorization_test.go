package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestExecuteProviderReplanDoesNotResetRelationOrderRiskBaseline(t *testing.T) {
	fixture := writeProviderOrderAuthorizationFixture(t)
	planned := fixture.initialPlan(t)
	runnerCalls := 0
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: fixture.providerExecutor(t, &runnerCalls, fixture.postProviderContent),
	})
	if !errors.Is(err, ErrRelationOrderRiskExpansion) {
		t.Fatalf("ExecuteWithOptions error = %v, want relation-order risk expansion", err)
	}
	if runnerCalls != 1 || len(result.HostRouteAttempts) != 1 {
		t.Fatalf(
			"provider calls=%d attempts=%#v, want one completed provider route",
			runnerCalls,
			result.HostRouteAttempts,
		)
	}
	fixture.assertSettings(t, fixture.postProviderContent)

	retry := fixture.plan(t)
	retryCalls := 0
	retryResult, err := ExecuteWithOptions(t.Context(), retry, ExecuteOptions{
		HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
				retryCalls++
				return subprocess.CommandResult{
					Started: true, HasExitCode: true, ExitCode: 0,
				}
			},
		}),
	})
	if err != nil {
		t.Fatalf("retry ExecuteWithOptions returned error: %v", err)
	}
	if retryCalls != 0 || len(retryResult.HostRouteAttempts) != 0 {
		t.Fatalf(
			"retry provider calls=%d attempts=%#v, want no provider replay",
			retryCalls,
			retryResult.HostRouteAttempts,
		)
	}
	fixture.assertSettings(t, fixture.convergedContent)
}

func TestExecuteProviderRiskExpansionSuppressesLaterDelegate(t *testing.T) {
	fixture := writeProviderOrderAuthorizationFixtureWithDelegate(t)
	planned := fixture.initialPlan(t)
	if len(planned.Reconciliation.Delegates()) != 1 {
		t.Fatalf(
			"initial delegates = %#v, want one later delegated action",
			planned.Reconciliation.Delegates(),
		)
	}

	providerCalls := 0
	delegateCalls := 0
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: fixture.providerExecutor(
			t,
			&providerCalls,
			fixture.postProviderContent,
		),
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
				delegateCalls++
				return subprocess.CommandResult{
					Started: true, HasExitCode: true, ExitCode: 0,
				}
			},
		}),
	})
	if !errors.Is(err, ErrRelationOrderRiskExpansion) {
		t.Fatalf("ExecuteWithOptions error = %v, want relation-order risk expansion", err)
	}
	if providerCalls != 1 || delegateCalls != 0 ||
		len(result.HostRouteAttempts) != 1 ||
		len(result.DelegateAttempts) != 0 {
		t.Fatalf(
			"provider calls=%d delegate calls=%d host attempts=%#v delegate attempts=%#v",
			providerCalls,
			delegateCalls,
			result.HostRouteAttempts,
			result.DelegateAttempts,
		)
	}
	fixture.assertSettings(t, fixture.postProviderContent)
}

func TestExecuteProviderRiskExpansionRequiresFreshAuthorization(t *testing.T) {
	authorizationErr := errors.New("confirmation output failed")
	for _, test := range []struct {
		name             string
		authorize        bool
		authorizationErr error
		wantErr          error
		wantContent      func(providerOrderAuthorizationFixture) string
		wantOutcome      RelationOrderOutcome
		wantChanged      bool
	}{
		{
			name:    "declined",
			wantErr: ErrRelationOrderNotAuthorized,
			wantContent: func(fixture providerOrderAuthorizationFixture) string {
				return fixture.postProviderContent
			},
			wantOutcome: RelationOrderNotAttempted,
		},
		{
			name:             "authorization error",
			authorizationErr: authorizationErr,
			wantErr:          authorizationErr,
			wantContent: func(fixture providerOrderAuthorizationFixture) string {
				return fixture.postProviderContent
			},
			wantOutcome: RelationOrderNotAttempted,
		},
		{
			name:      "accepted",
			authorize: true,
			wantContent: func(fixture providerOrderAuthorizationFixture) string {
				return fixture.convergedContent
			},
			wantOutcome: RelationOrderConverged,
			wantChanged: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := writeProviderOrderAuthorizationFixture(t)
			planned := fixture.initialPlan(t)
			runnerCalls := 0
			authorizationCalls := 0
			result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
				HostRouteExecutor: fixture.providerExecutor(
					t,
					&runnerCalls,
					fixture.postProviderContent,
				),
				RelationOrderRiskAuthorizer: func(
					_ context.Context,
					expansion RelationOrderRiskExpansion,
				) (bool, error) {
					authorizationCalls++
					if expansion.AddedRiskCount() != 2 ||
						len(expansion.Deltas()) != 1 {
						t.Fatalf("risk expansion = %#v, want two risks in one decision", expansion)
					}
					return test.authorize, test.authorizationErr
				},
			})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ExecuteWithOptions error = %v, want %v", err, test.wantErr)
			}
			if runnerCalls != 1 || authorizationCalls != 1 {
				t.Fatalf(
					"provider calls=%d authorization calls=%d, want one each",
					runnerCalls,
					authorizationCalls,
				)
			}
			if len(result.RelationOrderResults) != 1 ||
				result.RelationOrderResults[0].Outcome() != test.wantOutcome ||
				result.RelationOrderResults[0].Changed() != test.wantChanged {
				t.Fatalf(
					"relation order results = %#v, want outcome=%s changed=%t",
					result.RelationOrderResults,
					test.wantOutcome,
					test.wantChanged,
				)
			}
			fixture.assertSettings(t, test.wantContent(fixture))
		})
	}
}

func TestExecuteProviderReplanWithoutNewRiskDoesNotReauthorize(t *testing.T) {
	fixture := writeProviderOrderAuthorizationFixture(t)
	noRiskContent := `{"packages":["` + piProviderSource + `","` +
		fixture.betaSource + `","` + fixture.foreignSource + `"]}`
	planned := fixture.initialPlan(t)
	runnerCalls := 0
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: fixture.providerExecutor(t, &runnerCalls, noRiskContent),
		RelationOrderRiskAuthorizer: func(
			context.Context,
			RelationOrderRiskExpansion,
		) (bool, error) {
			t.Fatal("unchanged order risk requested fresh authorization")
			return false, nil
		},
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if runnerCalls != 1 || len(result.RelationOrderResults) != 1 ||
		result.RelationOrderResults[0].Outcome() != RelationOrderExact {
		t.Fatalf(
			"provider calls=%d relation order results=%#v, want one route and exact order",
			runnerCalls,
			result.RelationOrderResults,
		)
	}
	fixture.assertSettings(t, noRiskContent)
}

func TestExecuteProviderReplanRejectsStaleDeclarationBeforeOrderAuthorization(
	t *testing.T,
) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, providerOrderAuthorizationFixture)
	}{
		{
			name: "manifest",
			mutate: func(t *testing.T, fixture providerOrderAuthorizationFixture) {
				content, readErr := os.ReadFile(fixture.manifestPath)
				if readErr != nil {
					t.Fatalf("read manifest: %v", readErr)
				}
				changed := strings.Replace(
					string(content),
					"npm:@acme/beta@1.0.0",
					"npm:@acme/beta@2.0.0",
					1,
				)
				if changed == string(content) {
					t.Fatal("manifest fixture lacks beta source")
				}
				writeApplyFile(t, fixture.manifestPath, changed)
			},
		},
		{
			name: "lockfile",
			mutate: func(t *testing.T, fixture providerOrderAuthorizationFixture) {
				content, readErr := os.ReadFile(fixture.lockfilePath)
				if readErr != nil {
					t.Fatalf("read lockfile: %v", readErr)
				}
				writeApplyFile(t, fixture.lockfilePath, string(content)+"\n# changed\n")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := writeProviderOrderAuthorizationFixture(t)
			planned := fixture.initialPlan(t)
			runnerCalls := 0
			authorizationCalls := 0
			result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
				PlanWasDisclosed: true,
				HostRouteExecutor: fixture.providerExecutorAfter(
					t,
					&runnerCalls,
					fixture.postProviderContent,
					func() { test.mutate(t, fixture) },
				),
				RelationOrderRiskAuthorizer: func(
					context.Context,
					RelationOrderRiskExpansion,
				) (bool, error) {
					authorizationCalls++
					return true, nil
				},
			})
			var stale mutation.StalePlanError
			if !errors.As(err, &stale) {
				t.Fatalf("ExecuteWithOptions error = %v, want stale disclosed plan", err)
			}
			if runnerCalls != 1 || authorizationCalls != 0 ||
				len(result.HostRouteAttempts) != 1 ||
				len(result.RelationOrderResults) != 0 {
				t.Fatalf(
					"provider calls=%d authorization calls=%d attempts=%#v orders=%#v",
					runnerCalls,
					authorizationCalls,
					result.HostRouteAttempts,
					result.RelationOrderResults,
				)
			}
			fixture.assertSettings(t, fixture.postProviderContent)
		})
	}
}

func TestExecuteProviderReplanRejectsDeclarationChangeDuringRenewedConsent(
	t *testing.T,
) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, providerOrderAuthorizationFixture)
	}{
		{
			name: "manifest",
			mutate: func(t *testing.T, fixture providerOrderAuthorizationFixture) {
				content, err := os.ReadFile(fixture.manifestPath)
				if err != nil {
					t.Fatalf("read manifest: %v", err)
				}
				changed := strings.Replace(
					string(content),
					"must-not-run-daem-test",
					"changed-after-authorization",
					1,
				)
				if changed == string(content) {
					t.Fatal("manifest fixture lacks delegated command")
				}
				writeApplyFile(t, fixture.manifestPath, changed)
			},
		},
		{
			name: "lockfile",
			mutate: func(t *testing.T, fixture providerOrderAuthorizationFixture) {
				content, err := os.ReadFile(fixture.lockfilePath)
				if err != nil {
					t.Fatalf("read lockfile: %v", err)
				}
				writeApplyFile(t, fixture.lockfilePath, string(content)+"\n# changed\n")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := writeProviderOrderAuthorizationFixtureWithDelegate(t)
			planned := fixture.initialPlan(t)
			providerCalls := 0
			delegateCalls := 0
			authorizationCalls := 0
			result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
				PlanWasDisclosed: true,
				HostRouteExecutor: fixture.providerExecutor(
					t,
					&providerCalls,
					fixture.postProviderContent,
				),
				DelegateExecutor: delegate.NewExecutor(delegate.Options{
					Runner: func(
						context.Context,
						subprocess.CommandRequest,
					) subprocess.CommandResult {
						delegateCalls++
						return subprocess.CommandResult{
							Started: true, HasExitCode: true, ExitCode: 0,
						}
					},
				}),
				RelationOrderRiskAuthorizer: func(
					context.Context,
					RelationOrderRiskExpansion,
				) (bool, error) {
					authorizationCalls++
					test.mutate(t, fixture)
					return true, nil
				},
			})
			var stale mutation.StalePlanError
			if !errors.As(err, &stale) {
				t.Fatalf("ExecuteWithOptions error = %v, want stale disclosed plan", err)
			}
			if providerCalls != 1 || authorizationCalls != 1 || delegateCalls != 0 ||
				len(result.HostRouteAttempts) != 1 ||
				len(result.DelegateAttempts) != 0 ||
				len(result.RelationOrderResults) != 1 ||
				result.RelationOrderResults[0].Outcome() != RelationOrderNotAttempted {
				t.Fatalf(
					"provider=%d authorize=%d delegate=%d host_attempts=%#v delegate_attempts=%#v orders=%#v",
					providerCalls,
					authorizationCalls,
					delegateCalls,
					result.HostRouteAttempts,
					result.DelegateAttempts,
					result.RelationOrderResults,
				)
			}
			fixture.assertSettings(t, fixture.postProviderContent)
		})
	}
}

func TestExecuteProviderRouteFailurePreservesStaleDeclarationEvidence(
	t *testing.T,
) {
	fixture := writeProviderOrderAuthorizationFixture(t)
	planned := fixture.initialPlan(t)
	providerCalls := 0
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		PlanWasDisclosed: true,
		HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Runner: func(
				context.Context,
				subprocess.CommandRequest,
			) subprocess.CommandResult {
				providerCalls++
				content, readErr := os.ReadFile(fixture.manifestPath)
				if readErr != nil {
					t.Fatalf("read manifest during failed provider route: %v", readErr)
				}
				writeApplyFile(
					t,
					fixture.manifestPath,
					string(content)+"\n# changed during failed provider route\n",
				)
				return subprocess.CommandResult{
					Started: true, HasExitCode: true, ExitCode: 1,
				}
			},
		}),
	})
	var stale mutation.StalePlanError
	if !errors.As(err, &stale) ||
		!strings.Contains(err.Error(), "host route attempt failed") {
		t.Fatalf(
			"ExecuteWithOptions error = %v, want stale plan joined with route failure",
			err,
		)
	}
	if providerCalls != 1 || len(result.HostRouteAttempts) != 1 {
		t.Fatalf(
			"provider calls=%d attempts=%#v, want one retained failed attempt",
			providerCalls,
			result.HostRouteAttempts,
		)
	}
	fixture.assertSettings(t, fixture.initialContent)
}

func TestExecuteRejectsDeclarationChangeBetweenOrderAndDelegate(
	t *testing.T,
) {
	fixture := writeProviderOrderAuthorizationFixtureWithDelegate(t)
	planned := fixture.initialPlan(t)
	providerCalls := 0
	delegateCalls := 0
	orderCompleted := 0
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		PlanWasDisclosed: true,
		HostRouteExecutor: fixture.providerExecutor(
			t,
			&providerCalls,
			fixture.postProviderContent,
		),
		RelationOrderRiskAuthorizer: func(
			context.Context,
			RelationOrderRiskExpansion,
		) (bool, error) {
			return true, nil
		},
		ExecuteEvents: func(event execute.Event) {
			if event.Kind != execute.EventRelationOrderDone {
				return
			}
			orderCompleted++
			content, readErr := os.ReadFile(fixture.manifestPath)
			if readErr != nil {
				t.Fatalf("read manifest after order convergence: %v", readErr)
			}
			writeApplyFile(
				t,
				fixture.manifestPath,
				string(content)+"\n# changed before delegate\n",
			)
		},
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(
				context.Context,
				subprocess.CommandRequest,
			) subprocess.CommandResult {
				delegateCalls++
				return subprocess.CommandResult{
					Started: true, HasExitCode: true, ExitCode: 0,
				}
			},
		}),
	})
	var stale mutation.StalePlanError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteWithOptions error = %v, want stale disclosed plan", err)
	}
	if providerCalls != 1 || orderCompleted != 1 || delegateCalls != 0 ||
		len(result.HostRouteAttempts) != 1 ||
		len(result.RelationOrderResults) != 1 ||
		result.RelationOrderResults[0].Outcome() != RelationOrderConverged ||
		len(result.DelegateAttempts) != 0 {
		t.Fatalf(
			"provider=%d order_done=%d delegate=%d host_attempts=%#v orders=%#v delegates=%#v",
			providerCalls,
			orderCompleted,
			delegateCalls,
			result.HostRouteAttempts,
			result.RelationOrderResults,
			result.DelegateAttempts,
		)
	}
	fixture.assertSettings(t, fixture.convergedContent)
}

func TestExecutePreservesAttemptWhenDeclarationChangesDuringDelegate(
	t *testing.T,
) {
	fixture := writeProviderOrderAuthorizationFixtureWithDelegate(t)
	planned := fixture.initialPlan(t)
	providerCalls := 0
	delegateCalls := 0
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		PlanWasDisclosed: true,
		HostRouteExecutor: fixture.providerExecutor(
			t,
			&providerCalls,
			fixture.postProviderContent,
		),
		RelationOrderRiskAuthorizer: func(
			context.Context,
			RelationOrderRiskExpansion,
		) (bool, error) {
			return true, nil
		},
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(
				context.Context,
				subprocess.CommandRequest,
			) subprocess.CommandResult {
				delegateCalls++
				content, readErr := os.ReadFile(fixture.manifestPath)
				if readErr != nil {
					t.Fatalf("read manifest during delegate attempt: %v", readErr)
				}
				writeApplyFile(
					t,
					fixture.manifestPath,
					string(content)+"\n# changed during delegate attempt\n",
				)
				return subprocess.CommandResult{
					Started: true, HasExitCode: true, ExitCode: 0,
				}
			},
		}),
	})
	var stale mutation.StalePlanError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteWithOptions error = %v, want stale disclosed plan", err)
	}
	if providerCalls != 1 || delegateCalls != 1 ||
		len(result.HostRouteAttempts) != 1 ||
		len(result.RelationOrderResults) != 1 ||
		result.RelationOrderResults[0].Outcome() != RelationOrderConverged ||
		len(result.DelegateAttempts) != 1 {
		t.Fatalf(
			"provider=%d delegate=%d host_attempts=%#v orders=%#v delegates=%#v",
			providerCalls,
			delegateCalls,
			result.HostRouteAttempts,
			result.RelationOrderResults,
			result.DelegateAttempts,
		)
	}
	fixture.assertSettings(t, fixture.convergedContent)
}

func TestExecuteStopsDelegatesAfterDeclarationChanges(t *testing.T) {
	fixture := writeProviderOrderAuthorizationFixtureWithTwoDelegates(t)
	planned := fixture.initialPlan(t)
	if len(planned.Reconciliation.Delegates()) != 2 {
		t.Fatalf(
			"initial delegates = %#v, want two delegated actions",
			planned.Reconciliation.Delegates(),
		)
	}
	providerCalls := 0
	delegateCalls := 0
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		PlanWasDisclosed: true,
		HostRouteExecutor: fixture.providerExecutor(
			t,
			&providerCalls,
			fixture.postProviderContent,
		),
		RelationOrderRiskAuthorizer: func(
			context.Context,
			RelationOrderRiskExpansion,
		) (bool, error) {
			return true, nil
		},
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(
				context.Context,
				subprocess.CommandRequest,
			) subprocess.CommandResult {
				delegateCalls++
				content, readErr := os.ReadFile(fixture.manifestPath)
				if readErr != nil {
					t.Fatalf("read manifest during delegate attempt: %v", readErr)
				}
				writeApplyFile(
					t,
					fixture.manifestPath,
					string(content)+"\n# changed after first delegate\n",
				)
				return subprocess.CommandResult{
					Started: true, HasExitCode: true, ExitCode: 0,
				}
			},
		}),
	})
	var stale mutation.StalePlanError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteWithOptions error = %v, want stale disclosed plan", err)
	}
	if providerCalls != 1 || delegateCalls != 1 ||
		len(result.HostRouteAttempts) != 1 ||
		len(result.RelationOrderResults) != 1 ||
		result.RelationOrderResults[0].Outcome() != RelationOrderConverged ||
		len(result.DelegateAttempts) != 1 {
		t.Fatalf(
			"provider=%d delegate=%d host_attempts=%#v orders=%#v delegates=%#v",
			providerCalls,
			delegateCalls,
			result.HostRouteAttempts,
			result.RelationOrderResults,
			result.DelegateAttempts,
		)
	}
	fixture.assertSettings(t, fixture.convergedContent)
}

type providerOrderAuthorizationFixture struct {
	root                string
	manifestPath        string
	lockfilePath        string
	settingsPath        string
	betaSource          string
	foreignSource       string
	initialContent      string
	postProviderContent string
	convergedContent    string
}

func writeProviderOrderAuthorizationFixture(t *testing.T) providerOrderAuthorizationFixture {
	t.Helper()

	return writeProviderOrderAuthorizationFixtureWithOptions(t, false)
}

func writeProviderOrderAuthorizationFixtureWithDelegate(
	t *testing.T,
) providerOrderAuthorizationFixture {
	t.Helper()

	return writeProviderOrderAuthorizationFixtureWithOptions(t, true)
}

func writeProviderOrderAuthorizationFixtureWithTwoDelegates(
	t *testing.T,
) providerOrderAuthorizationFixture {
	t.Helper()

	fixture := writeProviderOrderAuthorizationFixtureWithDelegate(t)
	content, err := os.ReadFile(fixture.manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	writeApplyFile(t, fixture.manifestPath, string(content)+`

[[mcp_server]]
name = "delegated-second"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "must-not-run-second-daem-test"
args = ["--serve"]
`)
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
		ManifestPath: fixture.manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	return fixture
}

func writeProviderOrderAuthorizationFixtureWithOptions(
	t *testing.T,
	includeDelegate bool,
) providerOrderAuthorizationFixture {
	t.Helper()

	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	const betaSource = "npm:@acme/beta@1.0.0"
	const foreignSource = "../foreign-extension"

	targets := `["pi"]`
	delegateDeclaration := ""
	if includeDelegate {
		targets = `["pi", "claude-code"]`
		delegateDeclaration = `

[[mcp_server]]
name = "delegated"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "must-not-run-daem-test"
args = ["--serve"]
`
	}
	writeApplyFile(t, manifestPath, `version = 1
targets = `+targets+`

[[extension]]
id = "pi-mcp-adapter-project"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }

[[extension]]
id = "beta"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:@acme/beta@1.0.0" }

[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`+delegateDeclaration)
	initialContent := `{"packages":["` + betaSource + `","` + foreignSource + `"]}`
	writeApplyFile(t, settingsPath, initialContent)
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}

	return providerOrderAuthorizationFixture{
		root:                root,
		manifestPath:        manifestPath,
		lockfilePath:        filepath.Join(root, "daem.lock.toml"),
		settingsPath:        settingsPath,
		betaSource:          betaSource,
		foreignSource:       foreignSource,
		initialContent:      initialContent,
		postProviderContent: `{"packages":["` + betaSource + `","` + foreignSource + `","` + piProviderSource + `"]}`,
		convergedContent:    `{"packages":["` + piProviderSource + `","` + foreignSource + `","` + betaSource + `"]}`,
	}
}

func (fixture providerOrderAuthorizationFixture) plan(t *testing.T) *PreparedWrite {
	t.Helper()

	planned, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath:           fixture.manifestPath,
		ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	return planned
}

func (fixture providerOrderAuthorizationFixture) initialPlan(t *testing.T) *PreparedWrite {
	t.Helper()

	planned := fixture.plan(t)
	initialOrders := planned.Reconciliation.RelationOrders()
	if len(initialOrders) != 1 ||
		initialOrders[0].Kind() != reconcile.OrderConditionalAfterCarrierChange ||
		len(initialOrders[0].PrecedenceChanges()) != 0 {
		t.Fatalf("initial relation orders = %#v, want risk-free conditional plan", initialOrders)
	}
	return planned
}

func (fixture providerOrderAuthorizationFixture) providerExecutor(
	t *testing.T,
	calls *int,
	settingsContent string,
) subprocess.CommandExecutor {
	t.Helper()

	return fixture.providerExecutorAfter(t, calls, settingsContent, nil)
}

func (fixture providerOrderAuthorizationFixture) providerExecutorAfter(
	t *testing.T,
	calls *int,
	settingsContent string,
	after func(),
) subprocess.CommandExecutor {
	t.Helper()

	return subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			*calls = *calls + 1
			writeApplyFile(t, fixture.settingsPath, settingsContent)
			writePiProviderPackage(t, fixture.root, "2.15.0")
			if after != nil {
				after()
			}
			return subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: 0,
			}
		},
	})
}

func (fixture providerOrderAuthorizationFixture) assertSettings(t *testing.T, want string) {
	t.Helper()

	content, readErr := os.ReadFile(fixture.settingsPath)
	if readErr != nil {
		t.Fatalf("read Pi settings: %v", readErr)
	}
	if string(content) != want {
		t.Fatalf("Pi settings = %s, want %s", content, want)
	}
}
