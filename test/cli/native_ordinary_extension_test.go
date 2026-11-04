package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	daempaths "github.com/isty2e/daem/internal/paths"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeOrdinaryApplyWithCurrentExtension(t *testing.T) {
	for _, test := range []struct {
		name    string
		sources []string
	}{
		{name: "single", sources: []string{"git:github.com/acme/pi-tools@v1"}},
		{name: "ordered pair", sources: []string{"git:github.com/acme/pi-tools@v1", "git:github.com/acme/pi-more@v1"}},
	} {
		t.Run(test.name, func(t *testing.T) { testNativeOrdinaryApplyWithCurrentExtensions(t, test.sources) })
	}
}

func testNativeOrdinaryApplyWithCurrentExtensions(t *testing.T, sources []string) {
	t.Helper()
	fixture := nativefixture.NewPresentation(t, "model='native-test'\n", "foreign=true\n")
	workspace := filepath.Dir(fixture.Manifest)
	testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), "version=1\ntargets=['codex','pi']\n")
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
	for index, source := range sources {
		id := "tools"
		if index == 1 {
			id = "more"
		}
		nativefixture.RunLifecycleCommand(t, "add", "extension", id, source, "--manifest", fixture.Manifest, "--target", "pi", "--scope", "project")
	}
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
	routes := 0
	var installed []string
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
		routes++
		if request.Command != "pi" || len(request.Args) != 3 || request.Args[0] != "install" || request.Args[2] != "-l" || !slices.Contains(sources, request.Args[1]) {
			t.Fatalf("unexpected host route: %#v", request)
		}
		installed = append(installed, request.Args[1])
		content, err := json.Marshal(struct {
			Packages []string `json:"packages"`
		}{installed})
		if err != nil {
			t.Fatal(err)
		}
		testkit.WriteFile(t, workspace, ".pi/settings.json", string(content))
		return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
	}})
	run := func(mode string, jsonOutput bool) (int, string, string) {
		t.Helper()
		args := []string{"apply", "--manifest", fixture.Manifest, mode}
		if jsonOutput {
			args = append(args, "--json")
		}
		var stdout, stderr bytes.Buffer
		code := testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: executor}})
		return code, stdout.String(), stderr.String()
	}
	if code, stdout, stderr := run("--yes", false); code != 0 || routes != len(sources) {
		t.Fatalf("extension-only baseline failed: %d %s %s routes=%d", code, stdout, stderr, routes)
	}
	manifest := string(testkit.ReadFile(t, fixture.Manifest)) + "\n[instructions.project]\nsource='./instructions.md'\n[[host_config]]\nid='preferences'\ntargets=['codex']\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
	testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), manifest)
	for _, content := range []string{"first\n", "second\n"} {
		testkit.WriteFile(t, workspace, "instructions.md", content)
		nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
		for _, mode := range []string{"--dry-run", "--yes"} {
			code, stdout, stderr := run(mode, true)
			if code != 0 || !json.Valid([]byte(stdout)) || routes != len(sources) {
				t.Fatalf("current extension blocked mixed Apply or was rerun: %d %s %s routes=%d", code, stdout, stderr, routes)
			}
			if mode == "--yes" {
				var result struct {
					Order []struct {
						Target  string                             `json:"target"`
						Scope   string                             `json:"scope"`
						Outcome applyworkflow.RelationOrderOutcome `json:"outcome"`
						Changed bool                               `json:"changed"`
					} `json:"relation_order_results"`
				}
				if err := json.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatal(err)
				}
				if len(sources) == 1 && len(result.Order) != 0 {
					t.Fatal("singleton manufactured an order execution result", stdout)
				}
				if len(sources) > 1 && (len(result.Order) != 1 || result.Order[0].Target != "pi" || result.Order[0].Scope != "project" || result.Order[0].Outcome != applyworkflow.RelationOrderExact || result.Order[0].Changed) {
					t.Fatal("native direct Apply skipped the planned exact-order continuation", stdout)
				}
			}
		}
		if !bytes.Contains(testkit.ReadFile(t, filepath.Join(workspace, "AGENTS.md")), []byte(content)) {
			t.Fatal("current extension prevented the selected instruction update")
		}
		nativefixture.RunLifecycleCommand(t, "status", "--manifest", fixture.Manifest, "--check", "--json")
	}
	if err := os.Remove(filepath.Join(workspace, ".pi", "settings.json")); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, workspace, "instructions.md", "third\n")
	testkit.WriteFile(t, workspace, "preferences.toml", "model.child=true\n")
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
	before := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, filepath.Join(workspace, "AGENTS.md"), filepath.Join(workspace, ".daem"))
	for _, mode := range []string{"--dry-run", "--yes"} {
		code, stdout, stderr := run(mode, false)
		if code == 0 || routes != len(sources) {
			t.Fatalf("overlapping positive/retiring key cycle escaped its pre-effect refusal: %d %s %s routes=%d", code, stdout, stderr, routes)
		}
		if mode == "--dry-run" && (!strings.Contains(stdout, "not executable") || !strings.Contains(stderr, "apply preview failed:")) {
			t.Fatalf("refused preview lost its phase or affected plan: %s %s", stdout, stderr)
		}
		if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, filepath.Join(workspace, "AGENTS.md"), filepath.Join(workspace, ".daem"))) {
			t.Fatal("unsupported-route refusal changed the direct operation")
		}
	}
}

func TestNativePendingProjectCompletionAfterInstallInterruption(t *testing.T) {
	for _, scenario := range []string{"none", "host", "state", "empty fragment", "no pending", "different request"} {
		t.Run(scenario, func(t *testing.T) {
			boundary := scenario
			if scenario == "empty fragment" || scenario == "no pending" || scenario == "different request" {
				boundary = "none"
			}
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			fixture := nativefixture.RecoveryFixture{
				Manifest: filepath.Join(workspace, "daem.toml"), Destination: filepath.Join(workspace, ".codex", "config.toml"),
			}
			testkit.WriteFile(t, workspace, "daem.toml", "version=1\ntargets=['codex','pi']\n")
			testkit.WriteFile(t, workspace, "preferences.toml", "model='native-test'\n")
			fixture.Paths, err = daempaths.Resolve(fixture.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
			nativefixture.RunRecoveryCommand(t, fixture, "none", "add", "extension", "tools", "git:github.com/acme/pi-tools@v1", "--target", "pi", "--scope", "project")
			nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
			if code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "carrier-install", "apply", "--yes"); code != nativefixture.RecoveryExit {
				t.Fatalf("installation did not stop after host publication: %d %s %s", code, stdout, stderr)
			}
			before := nativefixture.ExtensionCompletionState(t, fixture.Paths)
			if len(before.PendingCarrierInstalls()) != 1 || len(before.ManagedCarrierClaims()) != 0 {
				t.Fatal("interrupted installation did not retain its exact pending fact")
			}
			installed := testkit.ReadFile(t, filepath.Join(workspace, ".pi", "settings.json"))
			if scenario == "empty fragment" {
				testkit.WriteFile(t, workspace, "preferences.toml", "")
			}
			if scenario == "no pending" || scenario == "different request" {
				var facts []durablecarrier.PendingCarrierInstall
				if scenario == "different request" {
					pending := before.PendingCarrierInstalls()[0]
					request, err := realizationdelegate.NewRequest(pending.InstallRequest().RouteID(), pending.InstallRequest().ContractVersion(), "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
					if err != nil {
						t.Fatal(err)
					}
					pending, err = durablecarrier.NewPendingCarrierInstall(pending.Owner(), pending.Identity(), request)
					if err != nil {
						t.Fatal(err)
					}
					facts = append(facts, pending)
				}
				before, err = before.WithPendingCarrierInstalls(facts)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := statefile.Marshal(before)
				if err != nil {
					t.Fatal(err)
				}
				testkit.WriteFile(t, filepath.Dir(fixture.Paths.StatefilePath), filepath.Base(fixture.Paths.StatefilePath), string(encoded))
			}
			manifest := string(testkit.ReadFile(t, fixture.Manifest)) + "\n[[host_config]]\nid='preferences'\ntargets=['codex']\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
			testkit.WriteFile(t, workspace, "daem.toml", manifest)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
			beforeRefusal := nativefixture.SnapshotRecoveryEvidence(t, fixture)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, boundary, "apply", "--yes", "--verbose")
			if scenario == "no pending" || scenario == "different request" {
				if code == 0 || code == nativefixture.RecoveryExit || !reflect.DeepEqual(beforeRefusal, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
					t.Fatalf("unmatched pending history admitted effects: %d %s %s", code, stdout, stderr)
				}
				return
			}
			if boundary == "none" {
				if code != 0 {
					t.Fatalf("mixed completion failed: %d %s %s", code, stdout, stderr)
				}
			} else {
				if code != nativefixture.RecoveryExit {
					t.Fatalf("mixed completion did not reach %s publication: %d %s %s", boundary, code, stdout, stderr)
				}
				classification := "needs_rollback"
				if boundary == "state" {
					classification = "needs_finalize"
				}
				published := nativefixture.ExtensionCompletionState(t, fixture.Paths)
				nativefixture.RequireRecoveryDryRun(t, fixture, classification)
				nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
				recovered := nativefixture.ExtensionCompletionState(t, fixture.Paths)
				if boundary == "host" {
					if !recovered.Equal(before) {
						t.Fatal("Before recovery lost pending completion provenance")
					}
				} else if !recovered.Equal(published) || len(recovered.PendingCarrierInstalls()) != 0 || len(recovered.ManagedCarrierClaims()) != 1 {
					t.Fatal("After recovery did not preserve the complete published completion")
				}
				nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
			}
			after := nativefixture.ExtensionCompletionState(t, fixture.Paths)
			if len(after.PendingCarrierInstalls()) != 0 || len(after.ManagedCarrierClaims()) != 1 || after.ContainsHostConfig() != (scenario != "empty fragment") {
				t.Fatalf("mixed successor lost completion: pending=%d claims=%d native=%t", len(after.PendingCarrierInstalls()), len(after.ManagedCarrierClaims()), after.ContainsHostConfig())
			}
			claim, pending := after.ManagedCarrierClaims()[0], before.PendingCarrierInstalls()[0]
			if !claim.Owner().ExactEqual(pending.Owner()) || !claim.Identity().ExactEqual(pending.Identity()) || !claim.InstallRequest().Equal(pending.InstallRequest()) || claim.Provenance() != durablecarrier.ClaimProvenanceInstalledObserved {
				t.Fatal("completion changed exact installation or acquisition provenance")
			}
			stateBeforeRepeat := testkit.ReadFile(t, fixture.Paths.StatefilePath)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
			if !bytes.Equal(stateBeforeRepeat, testkit.ReadFile(t, fixture.Paths.StatefilePath)) || !bytes.Equal(installed, testkit.ReadFile(t, filepath.Join(workspace, ".pi", "settings.json"))) {
				t.Fatal("repeat completion rewrote state or the installed extension")
			}
			nativefixture.RunRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}
