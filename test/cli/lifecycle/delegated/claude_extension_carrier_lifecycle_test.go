package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	clipkg "github.com/isty2e/daem/internal/cli"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/topology"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestClaudeExtensionCarrierPublicCLILockStatusApplyDiagnostics(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeExtensionManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("lock dry-run exitCode=%d, stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("lock dry-run stderr = %q, want empty", stderr.String())
	}
	lockPayload := clijson.DecodeLock(t, stdout.Bytes())
	if lockPayload.EntryCounts.Subjects != 1 {
		t.Fatalf("entry counts = %#v, want one subject only", lockPayload.EntryCounts)
	}
	if len(lockPayload.SubjectChanges) != 1 {
		t.Fatalf("subject_changes = %#v, want one", lockPayload.SubjectChanges)
	}
	subjectChange := lockPayload.SubjectChanges[0]
	if subjectChange.Status != "added" ||
		subjectChange.Subject.Kind != string(topology.SubjectHostRelation) ||
		subjectChange.Subject.Namespace != "claude-code.plugin-carrier" ||
		subjectChange.Subject.Name != "context7-managed" {
		t.Fatalf("subject change = %#v, want added Claude plugin carrier host relation", subjectChange)
	}
	if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
		t.Fatalf("dry-run lockfile stat err=%v, want no lockfile write", err)
	}
	assertNoCarrierSuccessClaims(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock write exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	assertCLIClaudeExtensionLockedSubject(t, lockfilePath)
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
		{
			name: "apply-dry-run-attempt-delegates-json",
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
			assertClaudeExtensionCarrierMissingCreateAction(t, payload.RelationActions)
			if strings.Contains(stdout.String(), `"claude_plugin_carrier_actions"`) {
				t.Fatalf("%s stdout = %s, want no legacy claude_plugin_carrier_actions field", command.name, stdout.String())
			}
			assertNoCarrierSuccessClaims(t, stdout.String())
		})
	}

	for _, command := range []struct {
		name string
		args []string
	}{
		{
			name: "status-human",
			args: []string{"status", "--manifest", manifestPath},
		},
		{
			name: "apply-dry-run-human",
			args: []string{"apply", "--manifest", manifestPath, "--dry-run"},
		},
	} {
		t.Run(command.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(command.args, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("%s exitCode=%d stdout=%q stderr=%q", command.name, exitCode, stdout.String(), stderr.String())
			}
			for _, want := range []string{
				"relation actions: 1 subjects",
				"kind=create",
				"execution=host_route",
				"correlation_state=missing",
				"correlation_reason=managed_relation_missing",
				"evidence_source=passive_relation_inventory",
				"evidence_availability=supported",
				"evidence_freshness=fresh",
				"replay_boundary=locked_route_request_identity_only",
				"retained_effects=",
				"non_claims=",
				"invokes_host_route=true",
				"allows_host_route_invocation=true",
				"blocks_ordinary_apply=false",
			} {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("%s stdout = %q, want %q", command.name, stdout.String(), want)
				}
			}
			assertNoCarrierSuccessClaims(t, stdout.String())
		})
	}
}

func TestClaudeExtensionCarrierPublicCLILockSeparatesSamePluginProjectAndGlobal(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeProjectAndGlobalSamePluginManifest())

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
	if len(locked.Locked.Subjects()) != 2 {
		t.Fatalf("locked subjects = %#v, want project and global Claude plugin carrier subjects", locked.Locked.Subjects())
	}

	managedKeys := make(map[string]string, 2)
	wantDeclarationByScope := map[string]string{
		"project": "context7-managed",
		"global":  "context7-global",
	}
	for _, record := range locked.Locked.Subjects() {
		relation := testkit.LockedDelegatedRelation(t, record)
		source, err := desiredextension.ParseSourceRef(relation.SourceNamespace())
		if err != nil {
			t.Fatalf("ParseSourceRef returned error: %v", err)
		}
		if relation.Target() != "claude-code" ||
			source.Ref() != "context7@market" ||
			string(relation.ExpectedRelation().SubjectKey()) != "context7@market" {
			t.Fatalf("relation = %#v, want shared context7 plugin key", relation)
		}
		scope := string(relation.Scope())
		if record.SubjectID().Key() != wantDeclarationByScope[scope] {
			t.Fatalf("subject = %#v, want declaration id %q for scope %q", record.SubjectID(), wantDeclarationByScope[scope], scope)
		}
		managedKeys[scope] = string(relation.ExpectedRelation().ManagedInstanceKey())
	}
	if managedKeys["project"] == "" || managedKeys["global"] == "" || managedKeys["project"] == managedKeys["global"] {
		t.Fatalf("managed keys = %#v, want distinct project/global managed relations", managedKeys)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("status --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	payload := clijson.DecodePlan(t, stdout.Bytes())
	seen := map[string]bool{}
	for _, action := range payload.RelationActions {
		if action.Subject != nil && action.Subject.Namespace == "claude-code.plugin-carrier" {
			seen[action.Subject.Name+"\x00"+action.Scope] = true
		}
	}
	if !seen["context7-managed\x00project"] || !seen["context7-global\x00global"] {
		t.Fatalf("relation actions = %#v, want project and global same-plugin rows", payload.RelationActions)
	}
	assertNoHostUserScopeLeak(t, stdout.String())
	assertNoCarrierSuccessClaims(t, stdout.String())
}

func TestClaudeExtensionCarrierPublicCLIApplyYesDelegatesAdmittedHostRoute(t *testing.T) {
	tests := []struct {
		name          string
		manifest      string
		declarationID string
		scope         string
		hostScope     observeclaudeplugin.HostScope
		wantArgs      []string
	}{
		{
			name:          "project",
			manifest:      claudeExtensionManifest(),
			declarationID: "context7-managed",
			scope:         "project",
			hostScope:     observeclaudeplugin.HostScopeProject,
			wantArgs:      []string{"plugin", "install", "context7@market", "--scope", "project"},
		},
		{
			name:          "global",
			manifest:      claudeGlobalExtensionManifest(),
			declarationID: "context7-global",
			scope:         "global",
			hostScope:     observeclaudeplugin.HostScopeUser,
			wantArgs:      []string{"plugin", "install", "context7@market", "--scope", "user"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			testkit.SetDataRootEnv(t, tempDir)
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			testkit.WriteFile(t, tempDir, "daem.toml", test.manifest)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("lock write exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			locked, err := lockfile.Load(t.Context(), lockfilePath)
			if err != nil {
				t.Fatalf("load lockfile: %v", err)
			}
			record := locked.Locked.Subjects()[0]
			subject := testkit.LockedDelegatedRelation(t, record)
			if record.SubjectID().Key() != test.declarationID || string(subject.Scope()) != test.scope {
				t.Fatalf("subject = %#v, want declaration=%s scope=%s", record.SubjectID(), test.declarationID, test.scope)
			}

			missingInventory := mustCLIClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
			})
			missingObservations := mustCLIClaudeObservationBatch(t, record.SubjectID(), subject, missingInventory)
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
						mustCLIClaudePluginManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), test.hostScope),
					},
				})
				return assurancehostroute.CurrentObservation(observeclaudeplugin.Correlate(subject, inventory))
			}

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
				t.Fatalf("apply --yes --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if len(requests) != 1 ||
				requests[0].Command != "claude" ||
				!slices.Equal(requests[0].Args, test.wantArgs) ||
				requests[0].WorkDir != tempDir {
				t.Fatalf("host route requests = %#v, want one Claude plugin install request with args %#v", requests, test.wantArgs)
			}
			applyPayload := clijson.DecodeApplyResult(t, stdout.Bytes())
			assertCLIClaudeHostRouteAttemptJSON(t, applyPayload.HostRouteAttempts, test.declarationID, test.scope, "attempted_observed_present", "observed_present")
			assertCLIHostRouteAttemptObservedPresentCommandSuccessJSON(t, applyPayload.HostRouteAttempts[0])
			if len(applyPayload.DelegateAttempts) != 0 {
				t.Fatalf("delegate_attempts = %#v, want none for carrier host route", applyPayload.DelegateAttempts)
			}
			assertNoHostUserScopeLeak(t, stdout.String())
			assertNoCarrierInstallConvergenceClaims(t, stdout.String())

			stdout.Reset()
			stderr.Reset()
			exitCode = testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("status --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			statusPayload := clijson.DecodePlan(t, stdout.Bytes())
			assertCLIClaudeHostRouteAttemptJSON(t, statusPayload.HostRouteAttempts, test.declarationID, test.scope, "attempted_observed_present", "observed_present")
			assertCLIHostRouteAttemptObservedPresentCommandSuccessJSON(t, statusPayload.HostRouteAttempts[0])
			assertNoHostUserScopeLeak(t, stdout.String())
			assertNoCarrierInstallConvergenceClaims(t, stdout.String())
		})
	}
}
