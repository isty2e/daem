package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/subprocess"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestClaudeGlobalExtensionCarrierDryRunRecoversFromManualDeletionHistory(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeGlobalExtensionManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one", locked.Locked.Subjects())
	}
	record := locked.Locked.Subjects()[0]
	carrierSubject := testkit.LockedDelegatedRelation(t, record)
	subject := record.SubjectID()
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	attempt := mustCLIObservedPresentHostRouteAttempt(
		t,
		subject,
		carrierSubject.Target(),
		carrierSubject.Scope(),
		carrierSubject.RouteID(),
		carrierSubject.CanonicalRequestHash(),
	)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		HostRouteAttempts: []durableattempt.HostRouteAttempt{attempt},
	})
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteStatefile(t, statefilePath, snapshot)
	stateBeforeDryRun, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("read state before dry-run: %v", err)
	}

	missingInventory := mustCLIClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	missingObservations := mustCLIClaudeObservationBatch(t, subject, carrierSubject, missingInventory)
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
				mustCLIClaudePluginManagedRowWithScope(t, "context7@market", string(carrierSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
			},
		})
		return assurancehostroute.CurrentObservation(observeclaudeplugin.Correlate(carrierSubject, inventory))
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				RelationObservations: &missingObservations,
				HostRouteExecutor:    executor,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply --dry-run --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 0 {
		t.Fatalf("dry-run invoked host route executor: %#v", requests)
	}
	stateAfterDryRun, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("read state after dry-run: %v", err)
	}
	if !bytes.Equal(stateAfterDryRun, stateBeforeDryRun) {
		t.Fatalf("dry-run mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBeforeDryRun, stateAfterDryRun)
	}
	payload := clijson.DecodePlan(t, stdout.Bytes())
	assertCLIHostRouteDryRunDisclosure(t, payload.RelationActions, hostRouteDisclosureExpectation{
		namespace: "claude-code.plugin-carrier",
		name:      "context7-global",
		target:    "claude-code",
		scope:     "global",
		routeID:   claudePluginRoute(t).RouteID(),
	})
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--yes", "--json"},
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
		t.Fatalf("apply --yes --json after dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 1 ||
		requests[0].Command != "claude" ||
		!slices.Equal(requests[0].Args, []string{"plugin", "install", "context7@market", "--scope", "user"}) ||
		requests[0].WorkDir != tempDir {
		t.Fatalf("host route requests after global dry-run = %#v, want one Claude user-scope install request", requests)
	}
	applyPayload := clijson.DecodeApplyResult(t, stdout.Bytes())
	assertCLIClaudeHostRouteAttemptJSON(t, applyPayload.HostRouteAttempts, "context7-global", "global", "attempted_observed_present", "observed_present")
	stateAfterApply, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("load statefile after mutating apply: %v", err)
	}
	attempts := stateAfterApply.HostRouteAttempts()
	if len(attempts) != 1 {
		t.Fatalf("persisted host route attempts = %#v, want one current mutating apply attempt", attempts)
	}
}

func TestClaudeGlobalExtensionCarrierPublicCLIApplyYesReportsAttemptedUnverifiedObservation(t *testing.T) {
	tests := []struct {
		name string
		json bool
	}{
		{
			name: "json",
			json: true,
		},
		{
			name: "human",
			json: false,
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
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					requests = append(requests, request)
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
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
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("apply --yes exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			assertCLIClaudeGlobalHostRouteRequest(t, fixture.root, requests)

			if test.json {
				payload := clijson.DecodeApplyResult(t, stdout.Bytes())
				assertCLIClaudeHostRouteAttemptJSON(t, payload.HostRouteAttempts, "context7-global", "global", "attempted_unverified", "observation_unavailable")
				assertCLIHostRouteAttemptAttemptedUnverifiedCommandSuccessJSON(t, payload.HostRouteAttempts[0])
				assertNoHostUserScopeLeak(t, stdout.String())
				assertNoCarrierInstallConvergenceClaims(t, stdout.String())
				return
			}

			output := stdout.String()
			for _, want := range []string{
				"host route attempts: 1 history-only diagnostics",
				"subject=\"host_relation/claude-code.plugin-carrier/context7-global\"",
				"target=claude-code",
				"scope=global",
				"route_id=\"" + claudePluginRoute(t).RouteID() + "\"",
				"result_class=attempted_unverified",
				"reason=observation_unavailable",
				"attempt_observed=true",
				"observation=not_observed",
				"postcondition=unknown",
				"exit_code=0",
				"grants_apply_skip_authority=false",
				"future_skip_authority",
				"package_cache_convergence",
				"runtime_readiness",
			} {
				if !strings.Contains(output, want) {
					t.Fatalf("apply --yes output = %q, want %q", output, want)
				}
			}
			if strings.Contains(output, "attempt_reason=") ||
				strings.Contains(output, "result_class=attempted_observed_present") ||
				strings.Contains(output, "observation=present") ||
				strings.Contains(output, "postcondition=observed") {
				t.Fatalf("apply --yes output = %q, want attempted-unverified without present upgrade", output)
			}
			assertNoHostUserScopeLeak(t, output)
			assertNoCarrierInstallConvergenceClaims(t, output)
		})
	}
}

func TestClaudeGlobalExtensionCarrierPublicCLIApplyYesDoesNotReusePriorPresentAttemptAsCurrentObservation(t *testing.T) {
	fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
	writeCLIClaudeGlobalObservedPresentAttemptStatefile(t, fixture)
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

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", fixture.manifestPath, "--yes", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				RelationObservations: &missingObservations,
				HostRouteExecutor:    executor,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply --yes --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	assertCLIClaudeGlobalHostRouteRequest(t, fixture.root, requests)

	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	assertCLIClaudeHostRouteAttemptJSON(t, payload.HostRouteAttempts, "context7-global", "global", "attempted_unverified", "observation_unavailable")
	assertCLIHostRouteAttemptAttemptedUnverifiedCommandSuccessJSON(t, payload.HostRouteAttempts[0])

	stateAfterApply, err := statefile.Load(t.Context(), filepath.Join(fixture.root, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("load statefile after mutating apply: %v", err)
	}
	attempts := stateAfterApply.HostRouteAttempts()
	if len(attempts) != 1 {
		t.Fatalf("state host_route_attempts = %#v, want one current attempted-unverified record", attempts)
	}
	attempt := attempts[0]
	exitCode, hasExitCode := attempt.ExitCode()
	if attempt.ResultClass() != durableattempt.HostRouteResultAttemptedUnverified ||
		attempt.Reason() != durableattempt.HostRouteReasonObservationUnavailable ||
		attempt.AttemptReason() != durableattempt.HostRouteAttemptReasonNone ||
		attempt.ObservationSummary() != observerelation.ObservationNotObserved ||
		attempt.PostconditionSummary() != observerelation.PostconditionUnknown ||
		!hasExitCode ||
		exitCode != 0 {
		t.Fatalf("state host_route_attempt = %#v, want current attempted-unverified history-only record", attempt)
	}
	assertNoHostUserScopeLeak(t, stdout.String())
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())
}

func TestClaudeGlobalExtensionCarrierPublicCLIApplyYesRejectsWrongScopePostAttemptObservation(t *testing.T) {
	tests := []struct {
		name string
		json bool
	}{
		{
			name: "json",
			json: true,
		},
		{
			name: "human",
			json: false,
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
						mustCLIClaudePluginManagedRowWithScope(t, "context7@market", string(fixture.subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
					},
				})
				return assurancehostroute.CurrentObservation(observeclaudeplugin.Correlate(fixture.subject, inventory))
			}

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
						HostRouteObserver:    observer,
					},
				},
			)
			if exitCode != 1 {
				t.Fatalf("apply --yes exitCode=%d stdout=%q stderr=%q, want 1", exitCode, stdout.String(), stderr.String())
			}
			assertCLIClaudeGlobalHostRouteRequest(t, fixture.root, requests)

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
					"attempted_observed_absent/observed_absent",
				} {
					if !strings.Contains(payload.Errors[0].Message, want) {
						t.Fatalf("json error message = %q, want %q", payload.Errors[0].Message, want)
					}
				}
				assertCLIClaudeHostRouteAttemptJSON(t, payload.HostRouteAttempts, "context7-global", "global", "attempted_observed_absent", "observed_absent")
				assertCLIHostRouteAttemptObservedAbsentCommandSuccessJSON(t, payload.HostRouteAttempts[0])
				assertNoHostUserScopeLeak(t, stdout.String())
				assertNoCarrierInstallConvergenceClaims(t, stdout.String())
				return
			}

			output := stderr.String()
			for _, want := range []string{
				"host route attempts: 1 history-only diagnostics",
				"subject=\"host_relation/claude-code.plugin-carrier/context7-global\"",
				"target=claude-code",
				"scope=global",
				"route_id=\"" + claudePluginRoute(t).RouteID() + "\"",
				"result_class=attempted_observed_absent",
				"reason=observed_absent",
				"attempt_observed=true",
				"observation=missing",
				"postcondition=missing",
				"exit_code=0",
				"grants_apply_skip_authority=false",
				"apply failed: host route attempt failed",
				"attempted_observed_absent/observed_absent",
			} {
				if !strings.Contains(output, want) {
					t.Fatalf("apply --yes stderr = %q, want %q; stdout=%q", output, want, stdout.String())
				}
			}
			combined := stdout.String() + stderr.String()
			for _, forbidden := range []string{
				"result_class=attempted_observed_present",
				"observation=present",
				"postcondition=observed",
			} {
				if strings.Contains(combined, forbidden) {
					t.Fatalf("apply --yes output = %q, want no wrong-scope present upgrade", combined)
				}
			}
			assertNoHostUserScopeLeak(t, combined)
			assertNoCarrierInstallConvergenceClaims(t, combined)
		})
	}
}

func TestClaudeExtensionCarrierPublicCLIDefaultObserverRetriesFreshAbsence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX host command fixture")
	}
	for _, test := range []struct {
		name     string
		manifest string
		scope    string
	}{
		{name: "project", manifest: claudeExtensionManifest(), scope: "project"},
		{name: "global", manifest: claudeGlobalExtensionManifest(), scope: "user"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDataRootEnv(t, root)
			manifestPath := filepath.Join(root, "daem.toml")
			configRoot := filepath.Join(root, "claude-config")
			callLog := filepath.Join(root, "claude-calls.log")
			binDir := filepath.Join(root, "bin")
			t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
			t.Setenv("DAEM_TEST_CLAUDE_CALL_LOG", callLog)
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			testkit.WriteFile(t, root, "daem.toml", test.manifest)
			testkit.WriteFile(t, binDir, "claude", `#!/bin/sh
set -eu
printf 'called\n' >> "$DAEM_TEST_CLAUDE_CALL_LOG"
mkdir -p "$CLAUDE_CONFIG_DIR/plugins"
if [ "$5" = "project" ]; then
  printf '{"version":2,"plugins":{"%s":[{"scope":"project","projectPath":"%s"}]}}\n' "$3" "$PWD" > "$CLAUDE_CONFIG_DIR/plugins/installed_plugins.json"
else
  printf '{"version":2,"plugins":{"%s":[{"scope":"user"}]}}\n' "$3" > "$CLAUDE_CONFIG_DIR/plugins/installed_plugins.json"
fi
`)
			if err := os.Chmod(filepath.Join(binDir, "claude"), 0o700); err != nil {
				t.Fatalf("chmod Claude fixture: %v", err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr); exitCode != 0 {
				t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}

			stdout.Reset()
			stderr.Reset()
			if exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr); exitCode != 0 {
				t.Fatalf("dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			testkit.AssertPathMissing(t, callLog)

			runApply := func(label string) {
				t.Helper()
				stdout.Reset()
				stderr.Reset()
				if exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes", "--json"}, &stdout, &stderr); exitCode != 0 || stderr.Len() != 0 {
					t.Fatalf("%s exitCode=%d stdout=%q stderr=%q", label, exitCode, stdout.String(), stderr.String())
				}
			}
			runApply("first apply")
			assertClaudeCallCount(t, callLog, 1)
			state, err := statefile.Load(t.Context(), filepath.Join(root, ".daem", "state.json"))
			if err != nil {
				t.Fatalf("load statefile: %v", err)
			}
			switch test.scope {
			case "project":
				claims := state.ManagedCarrierClaims()
				if len(claims) != 1 {
					t.Fatalf("project managed carrier claims = %#v, want one", claims)
				}
			case "user":
				if claims := state.ManagedCarrierClaims(); len(claims) != 0 {
					t.Fatalf("global claim leaked into project statefile: %#v", claims)
				}
				claims := loadCLIGlobalCarrierClaims(t, manifestPath)
				if len(claims) != 1 {
					t.Fatalf("global managed carrier claims = %#v, want one", claims)
				}
			default:
				t.Fatalf("unknown test scope %q", test.scope)
			}

			runApply("converged apply")
			assertClaudeCallCount(t, callLog, 1)

			testkit.WriteFile(t, configRoot, "plugins/installed_plugins.json", `{"version":2,"plugins":{}}`)
			runApply("fresh absence retry")
			assertClaudeCallCount(t, callLog, 2)
			installed, err := os.ReadFile(filepath.Join(configRoot, "plugins", "installed_plugins.json"))
			if err != nil {
				t.Fatalf("read installed inventory: %v", err)
			}
			if !strings.Contains(string(installed), `"scope":"`+test.scope+`"`) {
				t.Fatalf("installed inventory = %q, want host scope %q", installed, test.scope)
			}
		})
	}
}

func TestRunLockDoesNotPromoteCodexPluginConfigObservationToLockEntries(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]
`)
	testkit.WriteFile(t, homeDir, filepath.Join(".codex", "config.toml"), `
[plugins."alpha@market"]
enabled = true
`)
	pluginRoot := filepath.Join(homeDir, ".codex", "plugins", "cache", "market", "alpha", "local")
	testkit.WriteFile(t, pluginRoot, filepath.Join(".codex-plugin", "plugin.json"), `{
  "mcpServers": {"context7": {"command": "npx"}}
}`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	assertNoPluginDiagnosticAuthority(t, stdout.String())
	for _, forbidden := range []string{"alpha@market", "context7", "plugin_contributions", "source-declared"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("lock JSON leaked Codex plugin diagnostic fact %q:\n%s", forbidden, stdout.String())
		}
	}

	var payload clijson.Lock
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v\nstdout = %s", err, stdout.String())
	}
	if payload.EntryCounts.Subjects != 0 {
		t.Fatalf("entry counts = %#v, want empty lock", payload.EntryCounts)
	}
	if payload.ChangeCounts.Added != 0 || payload.ChangeCounts.Changed != 0 || payload.ChangeCounts.Removed != 0 || payload.ChangeCounts.Unchanged != 0 || payload.HasChanges {
		t.Fatalf("change counts = %#v hasChanges=%v, want no changes", payload.ChangeCounts, payload.HasChanges)
	}
	if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
		t.Fatalf("lockfile stat err=%v, want dry-run not to write lockfile", err)
	}
}
