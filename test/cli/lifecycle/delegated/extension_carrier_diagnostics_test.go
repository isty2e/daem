package cli_test

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestClaudeGlobalExtensionCarrierPublicCLIPreservesIdentityInDiagnostics(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeGlobalExtensionManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock --dry-run --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	lockPayload := clijson.DecodeLock(t, stdout.Bytes())
	assertLockJSONClaudePluginDelegatedRelation(t, lockPayload, "context7-global", "global")
	assertNoHostUserScopeLeak(t, stdout.String())
	assertNoCarrierSuccessClaims(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	assertCLIClaudeExtensionLockedSubjectWithScope(t, lockfilePath, "context7-global", "global")
	assertClaudePluginLockOutputFields(t, stdout.String(), "context7-global", "global")
	assertNoHostUserScopeLeak(t, stdout.String())
	assertNoCarrierSuccessClaims(t, stdout.String())

	for _, command := range []struct {
		name string
		args []string
	}{
		{
			name: "status-json",
			args: []string{"status", "--manifest", manifestPath, "--json"},
		},
		{
			name: "apply-dry-run-json",
			args: []string{"apply", "--manifest", manifestPath, "--dry-run", "--json"},
		},
	} {
		t.Run(command.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(command.args, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("%s exitCode=%d stdout=%q stderr=%q", command.name, exitCode, stdout.String(), stderr.String())
			}
			payload := clijson.DecodePlan(t, stdout.Bytes())
			if payload.HasErrors || payload.ActionCount != 0 || len(payload.Actions) != 0 || len(payload.DelegateActions) != 0 {
				t.Fatalf("%s payload = %#v, want carrier relation action without normal/delegate actions", command.name, payload)
			}
			assertClaudeExtensionCarrierMissingCreateActionWithScope(t, payload.RelationActions, "context7-global", "global")
			assertNoCarrierSuccessClaims(t, stdout.String())
		})
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("status human exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	assertClaudePluginStatusOutputFields(t, stdout.String(), "context7-global", "global")
	assertNoHostUserScopeLeak(t, stdout.String())
	assertNoCarrierSuccessClaims(t, stdout.String())
}

func TestClaudeGlobalExtensionCarrierPublicCLIApplyYesHumanReportsHostRouteAttempt(t *testing.T) {
	fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
	missingInventory := mustCLIClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	missingObservations := mustCLIClaudeObservationBatch(t, fixture.subjectID, fixture.subject, missingInventory)
	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
	observer := func(ctx context.Context, command executehostroute.Command, _ []durablecarrier.PendingCarrierInstall, _ []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
		inventory := mustCLIClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				mustCLIClaudePluginManagedRowWithScope(t, "context7@market", string(fixture.subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
			},
		})
		return assurancehostroute.CurrentObservation(observeclaudeplugin.Correlate(fixture.subject, inventory))
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", fixture.manifestPath, "--yes"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				RelationObservations: &missingObservations,
				HostRouteExecutor:    executor,
				HostRouteObserver:    observer,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply --yes exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	assertCLIClaudeGlobalHostRouteRequest(t, fixture.root, requests)

	output := stdout.String()
	if count := strings.Count(output, "relation actions: 1 subjects"); count != 1 {
		t.Fatalf("apply --yes output printed relation actions %d times, want once:\n%s", count, output)
	}
	for _, want := range []string{
		"applied: 0 actions",
		"host route attempts: 1 history-only diagnostics",
		"evidence=host_route_attempt_diagnostics",
		"authority=history_only",
		"subject=\"host_relation/claude-code.plugin-carrier/context7-global\"",
		"target=claude-code",
		"scope=global",
		"route_id=\"" + claudePluginRoute(t).RouteID() + "\"",
		"route_request_hash=\"sha256:",
		"result_class=attempted_observed_present",
		"reason=observed_present",
		"attempt_observed=true",
		"observation=present",
		"postcondition=observed",
		"exit_code=0",
		"grants_apply_skip_authority=false",
		"non_claims=",
		"future_skip_authority",
		"package_cache_convergence",
		"runtime_readiness",
		"statefile: " + filepath.Join(fixture.root, ".daem", "state.json"),
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("apply --yes output = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, "attempt_reason=") || strings.Contains(output, "redacted=true") {
		t.Fatalf("apply --yes output = %q, want clean success attempt without failure fields", output)
	}
	assertNoHostUserScopeLeak(t, output)
	assertNoCarrierInstallConvergenceClaims(t, output)
}

func TestClaudeGlobalExtensionCarrierPublicCLIApplyYesReportsCommandFailure(t *testing.T) {
	tests := []struct {
		name     string
		json     bool
		exitCode int
	}{
		{
			name:     "json",
			json:     true,
			exitCode: 1,
		},
		{
			name:     "human",
			json:     false,
			exitCode: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
			missingInventory := mustCLIClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
			})
			missingObservations := mustCLIClaudeObservationBatch(t, fixture.subjectID, fixture.subject, missingInventory)
			var requests []subprocess.CommandRequest
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				OutputLimit: 24,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					requests = append(requests, request)
					return subprocess.CommandResult{
						Started:     true,
						HasExitCode: true,
						ExitCode:    17,
						Stdout:      "token=super-secret" + strings.Repeat(" api_key=literal-token", 20),
						Stderr:      `password=hunter2 shell="$(touch should-not-run)"`,
					}
				},
			})

			args := []string{"apply", "--manifest", fixture.manifestPath, "--yes"}
			if test.json {
				args = append(args, "--json")
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLIWithOptions(
				args,
				clipkg.RunOptions{
					Stdout: &stdout,
					Stderr: &stderr,
					ApplyExecuteOptions: applyworkflow.ExecuteOptions{
						RelationObservations: &missingObservations,
						HostRouteExecutor:    executor,
					},
				},
			)
			if exitCode != test.exitCode {
				t.Fatalf("apply --yes exitCode=%d stdout=%q stderr=%q, want %d", exitCode, stdout.String(), stderr.String(), test.exitCode)
			}
			assertCLIClaudeGlobalHostRouteRequest(t, fixture.root, requests)
			assertNoHostRouteRawFailureOutputLeak(t, stdout.String()+stderr.String())

			if test.json {
				if stderr.Len() != 0 {
					t.Fatalf("apply --yes --json stderr = %q, want empty", stderr.String())
				}
				payload := clijson.DecodeApplyResult(t, stdout.Bytes())
				if !payload.HasErrors || len(payload.Errors) != 1 {
					t.Fatalf("apply --yes --json payload = %#v, want one error", payload)
				}
				for _, want := range []string{
					"host route attempt failed",
					"host_relation/claude-code.plugin-carrier",
					"context7-global",
					"failed/nonzero_exit",
				} {
					if !strings.Contains(payload.Errors[0].Message, want) {
						t.Fatalf("json error message = %q, want %q", payload.Errors[0].Message, want)
					}
				}
				assertCLIClaudeHostRouteAttemptJSON(t, payload.HostRouteAttempts, "context7-global", "global", "failed", "nonzero_exit")
				attempt := payload.HostRouteAttempts[0]
				if attempt.AttemptReason != "nonzero_exit" ||
					attempt.ExitCode == nil ||
					*attempt.ExitCode != 17 ||
					!attempt.Redacted ||
					attempt.Observation != "not_observed" ||
					attempt.Postcondition != "not_observed" {
					t.Fatalf("json host_route_attempt = %#v, want bounded failed command diagnostic", attempt)
				}
				assertNoHostUserScopeLeak(t, stdout.String())
				assertNoCarrierInstallConvergenceClaims(t, stdout.String())
				return
			}

			if stdout.Len() == 0 {
				t.Fatal("apply --yes human stdout is empty, want pre-execution relation action disclosure")
			}
			output := stderr.String()
			for _, want := range []string{
				"host route attempts: 1 history-only diagnostics",
				"subject=\"host_relation/claude-code.plugin-carrier/context7-global\"",
				"target=claude-code",
				"scope=global",
				"route_id=\"" + claudePluginRoute(t).RouteID() + "\"",
				"result_class=failed",
				"reason=nonzero_exit",
				"attempt_observed=true",
				"observation=not_observed",
				"postcondition=not_observed",
				"attempt_reason=nonzero_exit",
				"exit_code=17",
				"redacted=true",
				"grants_apply_skip_authority=false",
				"apply failed: host route attempt failed",
				"failed/nonzero_exit",
			} {
				if !strings.Contains(output, want) {
					t.Fatalf("apply --yes stderr = %q, want %q; stdout=%q", output, want, stdout.String())
				}
			}
			assertNoHostUserScopeLeak(t, stdout.String()+stderr.String())
			assertNoCarrierInstallConvergenceClaims(t, stdout.String()+stderr.String())
		})
	}
}

func assertNoHostRouteRawFailureOutputLeak(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{
		"super-secret",
		"literal-token",
		"hunter2",
		"should-not-run",
		"api_key=",
		"password=",
		"token=",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output leaked raw host route failure payload %q:\n%s", forbidden, output)
		}
	}
}

func assertClaudePluginLockOutputFields(t *testing.T, output string, declarationID string, scope string) {
	t.Helper()
	for _, want := range []string{
		"host_relation/" + "claude-code.plugin-carrier" + "/" + declarationID,
		"realization=\"delegated_relation\"",
		"target=\"claude-code\"",
		"scope=\"" + scope + "\"",
		"relation_subject_key=\"context7@market\"",
		"route_id=\"" + claudePluginRoute(t).RouteID() + "\"",
		"route_contract=\"" + claudePluginRoute(t).AdapterContractVersion() + "\"",
		"request_hash=\"sha256:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("lock output = %q, want %q", output, want)
		}
	}
}

func assertClaudePluginStatusOutputFields(t *testing.T, output string, declarationID string, scope string) {
	t.Helper()
	for _, want := range []string{
		"relation actions: 1 subjects",
		"subject=\"host_relation/" + "claude-code.plugin-carrier" + "/" + declarationID + "\"",
		"target=claude-code",
		"scope=" + scope,
		"source_kind=\"marketplace\"",
		"source_ref=\"context7@market\"",
		"source_namespace=\"marketplace:context7@market\"",
		"relation_subject_key=\"context7@market\"",
		"route_id=\"" + claudePluginRoute(t).RouteID() + "\"",
		"replay_boundary=locked_route_request_identity_only",
		"runtime_readiness",
		"carrier_removal",
		"future_skip_authority",
		"invokes_host_route=true",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status output = %q, want %q", output, want)
		}
	}
}

func assertNoHostUserScopeLeak(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{
		"scope=user",
		"scope = \"user\"",
		`"scope": "user"`,
		"--scope user",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output contains host user scope leak %q:\n%s", forbidden, output)
		}
	}
}

func assertRelationDisclosureSlice(t *testing.T, label string, got []string, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
}

func assertNoCarrierSuccessClaims(t *testing.T, output string) {
	t.Helper()

	for _, forbidden := range []string{
		"installed",
		"enabled",
		"ready",
		"converged",
		"applied",
	} {
		if containsStandaloneWord(output, forbidden) {
			t.Fatalf("output contains forbidden carrier success/execution claim %q:\n%s", forbidden, output)
		}
	}
}

func assertNoCarrierInstallConvergenceClaims(t *testing.T, output string) {
	t.Helper()

	for _, forbidden := range []string{
		"installed",
		"synced",
		"ready",
		"converged",
	} {
		if containsStandaloneWord(output, forbidden) {
			t.Fatalf("output contains forbidden carrier convergence claim %q:\n%s", forbidden, output)
		}
	}
}

func containsStandaloneWord(output string, word string) bool {
	return slices.Contains(strings.FieldsFunc(strings.ToLower(output), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_'
	}), word)
}

func assertNoClaudePluginUpdateClaims(t *testing.T, output string) {
	t.Helper()

	lower := strings.ToLower(output)
	for _, forbidden := range []string{
		"claude plugin update",
		"plugin update route",
		"plugin upgrade",
		"upgrade-capable",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("output contains forbidden Claude plugin update claim %q:\n%s", forbidden, output)
		}
	}
}
