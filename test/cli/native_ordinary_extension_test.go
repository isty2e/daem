package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	daempaths "github.com/isty2e/daem/internal/paths"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeOrdinaryApplyWithCurrentExtension(t *testing.T) {
	fixture := nativePresentationFixture(t, "model='native-test'\n", "foreign=true\n")
	workspace := filepath.Dir(fixture.manifest)
	const source = "git:github.com/acme/pi-tools@v1"
	testkit.WriteFile(t, workspace, filepath.Base(fixture.manifest), "version=1\ntargets=['codex','pi']\n")
	runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
	runNativeLifecycleCommand(t, "add", "extension", "tools", source, "--manifest", fixture.manifest, "--target", "pi", "--scope", "project")
	runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
	routes := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
		routes++
		if request.Command != "pi" || !reflect.DeepEqual(request.Args, []string{"install", source, "-l"}) {
			t.Fatalf("unexpected host route: %#v", request)
		}
		testkit.WriteFile(t, workspace, ".pi/settings.json", `{"packages":["`+source+`"]}`)
		return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
	}})
	run := func(mode string, jsonOutput bool) (int, string, string) {
		t.Helper()
		args := []string{"apply", "--manifest", fixture.manifest, mode}
		if jsonOutput {
			args = append(args, "--json")
		}
		var stdout, stderr bytes.Buffer
		code := testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: executor}})
		return code, stdout.String(), stderr.String()
	}
	if code, stdout, stderr := run("--yes", false); code != 0 || routes != 1 {
		t.Fatalf("extension-only baseline failed: %d %s %s routes=%d", code, stdout, stderr, routes)
	}
	manifest := string(testkit.ReadFile(t, fixture.manifest)) + "\n[instructions.project]\nsource='./instructions.md'\n[[host_config]]\nid='preferences'\ntargets=['codex']\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
	testkit.WriteFile(t, workspace, filepath.Base(fixture.manifest), manifest)
	for _, content := range []string{"first\n", "second\n"} {
		testkit.WriteFile(t, workspace, "instructions.md", content)
		runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
		for _, mode := range []string{"--dry-run", "--yes"} {
			if code, stdout, stderr := run(mode, true); code != 0 || !json.Valid([]byte(stdout)) || routes != 1 {
				t.Fatalf("current extension blocked mixed Apply or was rerun: %d %s %s routes=%d", code, stdout, stderr, routes)
			}
		}
		if !bytes.Contains(testkit.ReadFile(t, filepath.Join(workspace, "AGENTS.md")), []byte(content)) {
			t.Fatal("current extension prevented the selected instruction update")
		}
		runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json")
	}
	if err := os.Remove(filepath.Join(workspace, ".pi", "settings.json")); err != nil {
		t.Fatal(err)
	}
	before := snapshotNativeRecoveryPaths(t, fixture.destination, filepath.Join(workspace, "AGENTS.md"), filepath.Join(workspace, ".daem"))
	for _, mode := range []string{"--dry-run", "--yes"} {
		code, stdout, stderr := run(mode, false)
		if code == 0 || routes != 1 || !strings.Contains(stdout+stderr, "extension or host-registration changes") {
			t.Fatalf("changed extension escaped the unsupported-route boundary: %d %s %s routes=%d", code, stdout, stderr, routes)
		}
		if mode == "--dry-run" && (!strings.Contains(stdout, "not executable") || !strings.Contains(stderr, "apply preview failed:")) {
			t.Fatalf("refused preview lost its phase or affected plan: %s %s", stdout, stderr)
		}
		if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.destination, filepath.Join(workspace, "AGENTS.md"), filepath.Join(workspace, ".daem"))) {
			t.Fatal("unsupported-route refusal changed the direct operation")
		}
	}
}

func nativeExtensionCompletionState(t *testing.T, paths daempaths.Paths) durable.Snapshot {
	t.Helper()
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	state, err := statefile.LoadOptionalWithOptions(t.Context(), paths.StatefilePath, statefile.ReadOptions{
		Codec: codec, PhysicalPathBudget: recovery.NewPhysicalCaptureBudget(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
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
			setNativeRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			fixture := nativeRecoveryFixture{
				manifest: filepath.Join(workspace, "daem.toml"), destination: filepath.Join(workspace, ".codex", "config.toml"),
			}
			testkit.WriteFile(t, workspace, "daem.toml", "version=1\ntargets=['codex','pi']\n")
			testkit.WriteFile(t, workspace, "preferences.toml", "model='native-test'\n")
			fixture.paths, err = daempaths.Resolve(fixture.manifest)
			if err != nil {
				t.Fatal(err)
			}
			runNativeRecoveryCommand(t, fixture, "none", "lock")
			runNativeRecoveryCommand(t, fixture, "none", "add", "extension", "tools", "git:github.com/acme/pi-tools@v1", "--target", "pi", "--scope", "project")
			runNativeRecoveryCommand(t, fixture, "none", "lock")
			if code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "carrier-install", "apply", "--yes"); code != nativeRecoveryExit {
				t.Fatalf("installation did not stop after host publication: %d %s %s", code, stdout, stderr)
			}
			before := nativeExtensionCompletionState(t, fixture.paths)
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
				testkit.WriteFile(t, filepath.Dir(fixture.paths.StatefilePath), filepath.Base(fixture.paths.StatefilePath), string(encoded))
			}
			manifest := string(testkit.ReadFile(t, fixture.manifest)) + "\n[[host_config]]\nid='preferences'\ntargets=['codex']\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
			testkit.WriteFile(t, workspace, "daem.toml", manifest)
			runNativeRecoveryCommand(t, fixture, "none", "lock")
			beforeRefusal := snapshotNativeRecoveryEvidence(t, fixture)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, "apply", "--yes", "--verbose")
			if scenario == "no pending" || scenario == "different request" {
				if code == 0 || code == nativeRecoveryExit || !reflect.DeepEqual(beforeRefusal, snapshotNativeRecoveryEvidence(t, fixture)) {
					t.Fatalf("unmatched pending history admitted effects: %d %s %s", code, stdout, stderr)
				}
				return
			}
			if boundary == "none" {
				if code != 0 {
					t.Fatalf("mixed completion failed: %d %s %s", code, stdout, stderr)
				}
			} else {
				if code != nativeRecoveryExit {
					t.Fatalf("mixed completion did not reach %s publication: %d %s %s", boundary, code, stdout, stderr)
				}
				classification := "needs_rollback"
				if boundary == "state" {
					classification = "needs_finalize"
				}
				published := nativeExtensionCompletionState(t, fixture.paths)
				requireNativeRecoveryDryRun(t, fixture, classification)
				runNativeRecoveryCommand(t, fixture, "none", "recover", "--yes")
				recovered := nativeExtensionCompletionState(t, fixture.paths)
				if boundary == "host" {
					if !recovered.Equal(before) {
						t.Fatal("Before recovery lost pending completion provenance")
					}
				} else if !recovered.Equal(published) || len(recovered.PendingCarrierInstalls()) != 0 || len(recovered.ManagedCarrierClaims()) != 1 {
					t.Fatal("After recovery did not preserve the complete published completion")
				}
				runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
			}
			after := nativeExtensionCompletionState(t, fixture.paths)
			if len(after.PendingCarrierInstalls()) != 0 || len(after.ManagedCarrierClaims()) != 1 || after.ContainsHostConfig() != (scenario != "empty fragment") {
				t.Fatalf("mixed successor lost completion: pending=%d claims=%d native=%t", len(after.PendingCarrierInstalls()), len(after.ManagedCarrierClaims()), after.ContainsHostConfig())
			}
			claim, pending := after.ManagedCarrierClaims()[0], before.PendingCarrierInstalls()[0]
			if !claim.Owner().ExactEqual(pending.Owner()) || !claim.Identity().ExactEqual(pending.Identity()) || !claim.InstallRequest().Equal(pending.InstallRequest()) || claim.Provenance() != durablecarrier.ClaimProvenanceInstalledObserved {
				t.Fatal("completion changed exact installation or acquisition provenance")
			}
			stateBeforeRepeat := testkit.ReadFile(t, fixture.paths.StatefilePath)
			runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
			if !bytes.Equal(stateBeforeRepeat, testkit.ReadFile(t, fixture.paths.StatefilePath)) || !bytes.Equal(installed, testkit.ReadFile(t, filepath.Join(workspace, ".pi", "settings.json"))) {
				t.Fatal("repeat completion rewrote state or the installed extension")
			}
			runNativeRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}
