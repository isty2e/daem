package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestPiSourceHandoffConvergesMovingGitToNPMAndProviderSelector(t *testing.T) {
	for _, test := range []struct {
		name, before, after                         string
		native, provider, failProvider, failRemoval bool
	}{
		{"moving-git-global", "git:github.com/example/package@main", "npm:replacement-tools@1.0.0", false, false, false, false},
		{"native-moving-git-global", "git:github.com/example/package@main", "npm:replacement-tools@1.0.0", true, false, false, false},
		{"provider-selector", piProviderSource, "npm:pi-mcp-adapter@^2.14.0", false, true, false, false},
		{"provider-install-failure", piProviderSource, "npm:pi-mcp-adapter@^2.14.0", false, true, true, false},
		{"native-removal-failure", "git:github.com/example/package@main", "npm:replacement-tools@1.0.0", true, false, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.native && runtime.GOOS == "windows" {
				t.Skip("fixture uses a POSIX shell host executable")
			}
			root := newApplyCarrierFixtureRoot(t)
			agent := filepath.Join(root, "pi-agent")
			t.Setenv("PI_CODING_AGENT_DIR", agent)
			manifest := filepath.Join(root, "daem.toml")
			writeApplyFile(t, manifest, fmt.Sprintf("version=1\ntargets=['pi']\n[[extension]]\nid='tools-managed'\ncarrier='pi-package'\nscope='global'\nsource={host_source=%q}\n", test.after))
			if test.native {
				contents, err := os.ReadFile(manifest)
				if err != nil {
					t.Fatal(err)
				}
				writeApplyFile(t, filepath.Join(root, "preferences.json"), `{"theme":"handoff-native"}`)
				writeApplyFile(t, manifest, string(contents)+fmt.Sprintf("\n[[host_config]]\nid='preferences'\nscope='global'\npath='settings.json'\nsource=%q\nownership='keys'\n", filepath.Join(root, "preferences.json")))
			}
			if test.provider {
				contents, err := os.ReadFile(manifest)
				if err != nil {
					t.Fatal(err)
				}
				writeApplyFile(t, manifest, string(contents)+"\n[[mcp_server]]\nname='search'\ntransport='stdio'\nscope='global'\ncommand='node'\nargs=['search.js']\n")
				writeApplyFile(t, filepath.Join(agent, "npm", "node_modules", "pi-mcp-adapter", "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0"}`)
			}
			seedApplyCarrierClaim(t, root, manifest, piPinApplyLock(t, target.ScopeGlobal, test.before), target.ScopeGlobal)
			settings, err := observepipackage.SettingsPath(observepipackage.SettingsInput{WorkDir: root, ProjectRoot: root, Scope: target.ScopeGlobal})
			if err != nil {
				t.Fatal(err)
			}
			writeApplyFile(t, settings, fmt.Sprintf(`{"packages":[%q],"unmanaged":true}`, test.before))
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
				t.Fatal(err)
			}
			prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if prepared.Reconciliation.HasErrors() {
				t.Fatal("admitted conditional handoff was reported as a blocked plan")
			}
			if test.provider && !prepared.Reconciliation.HasBlockedRelations() {
				t.Fatal("conditional planning relabeled the actual same-name inventory")
			}
			calls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				want := []string{"remove", test.before}
				if calls == 2 {
					want = []string{"install", test.after}
				}
				if request.Command != "pi" || calls > 2 || !slices.Equal(request.Args, want) {
					t.Fatalf("unexpected command: %s %v", request.Command, request.Args)
				}
				content, err := os.ReadFile(settings)
				if err != nil {
					t.Fatal(err)
				}
				var document map[string]json.RawMessage
				if err := json.Unmarshal(content, &document); err != nil {
					t.Fatal(err)
				}
				if calls == 1 {
					document["packages"] = json.RawMessage(`[]`)
					if test.provider {
						if err := os.RemoveAll(filepath.Join(agent, "npm", "node_modules", "pi-mcp-adapter")); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					if len(loadCarrierClaimsForScope(t, root, manifest, target.ScopeGlobal)) != 0 {
						t.Fatal("old global claim survived until new install")
					}
					if test.failProvider {
						writeApplyFile(t, settings, "{unavailable-after-install")
						return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 23}
					}
					encoded, err := json.Marshal([]string{test.after})
					if err != nil {
						t.Fatal(err)
					}
					document["packages"] = encoded
					if test.provider {
						writeApplyFile(t, filepath.Join(agent, "npm", "node_modules", "pi-mcp-adapter", "package.json"), `{"name":"pi-mcp-adapter","version":"2.16.0"}`)
					}
				}
				content, err = json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				writeApplyFile(t, settings, string(content))
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			if test.native {
				binary := filepath.Join(root, "bin", "pi")
				script := fmt.Sprintf(`#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.0.2\n'; exit 0; fi
grep -q handoff-native %q || exit 91
printf '%%s\n' "$1" >> %q
case "$1" in
remove) printf '%%s\n' '{"theme":"handoff-native","packages":[],"unmanaged":true}' > %q ;;
install) printf '%%s\n' '{"theme":"handoff-native","packages":[%q],"unmanaged":true}' > %q ;;
*) exit 92 ;;
esac
`, settings, filepath.Join(root, "host-calls"), settings, test.after, settings)
				writeApplyFile(t, binary, script)
				if test.failRemoval {
					writeApplyFile(t, binary, strings.Replace(script, "case \"$1\" in", "case \"$1\" in\nremove) exit 23 ;;", 1))
				}
				if err := os.Chmod(binary, 0o700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
				executor = subprocess.NewCommandExecutor(subprocess.CommandOptions{})
			}
			result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{HostRouteExecutor: executor})
			if test.failRemoval {
				if err == nil || len(result.HostRouteAttempts) != 1 || result.StatefilePath == "" || !result.UncompensatedEffectsAttempted ||
					result.CarrierHandoffProgress == nil || len(result.CarrierHandoffProgress.RetiredClaims) != 0 || result.CarrierHandoffProgress.Requalified || result.CarrierHandoffProgress.Complete {
					t.Fatal("native failure lost the actual pending-removal result", err)
				}
				state := loadApplyStatefile(t, result.StatefilePath)
				if len(state.PendingCarrierRemovals()) != 1 || len(state.PendingCarrierInstalls()) != 0 || len(loadCarrierClaimsForScope(t, root, manifest, target.ScopeGlobal)) != 1 {
					t.Fatal("native failure discarded old management or started acquisition")
				}
				return
			}
			if test.failProvider {
				if err == nil || calls != 2 || len(result.HostRouteAttempts) != 2 || result.ActionCount == 0 || result.StatefilePath == "" ||
					result.CarrierHandoffProgress == nil || len(result.CarrierHandoffProgress.RetiredClaims) != 1 || !result.CarrierHandoffProgress.Requalified || result.CarrierHandoffProgress.Complete ||
					result.HostRouteAttempts[0].Operation() != lock.OperationRemove || result.HostRouteAttempts[1].Operation() != lock.OperationInstall {
					t.Fatal("failed provider hid retirement progress or reordered its attempts", err)
				}
				state := loadApplyStatefile(t, result.StatefilePath)
				if len(state.PendingCarrierInstalls()) != 0 || len(state.PendingCarrierRemovals()) != 0 || len(state.HostRouteAttempts()) != 2 || len(loadCarrierClaimsForScope(t, root, manifest, target.ScopeGlobal)) != 0 {
					t.Fatal("provider failure lost the committed retirement or failed-attempt history")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.native {
				content, err := os.ReadFile(filepath.Join(root, "host-calls"))
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != "remove\ninstall\n" {
					t.Fatalf("native commands out of order: %s", content)
				}
				calls = 2
			}
			claims := loadCarrierClaimsForScope(t, root, manifest, target.ScopeGlobal)
			if len(result.HostRouteAttempts) != 2 || result.CarrierHandoffProgress == nil || len(result.CarrierHandoffProgress.RetiredClaims) != 1 || !result.CarrierHandoffProgress.Requalified || !result.CarrierHandoffProgress.Complete || result.HostRouteAttempts[0].Operation() != lock.OperationRemove || result.HostRouteAttempts[1].Operation() != lock.OperationInstall {
				t.Fatal("completed handoff omitted progress or reordered attempts")
			}
			if calls != 2 || len(result.HostRouteAttempts) != 2 || len(claims) != 1 || claims[0].Identity().Carrier().Key().Source().Ref() != test.after {
				t.Fatal("Pi source handoff did not converge")
			}
			content, err := os.ReadFile(settings)
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Packages  []string `json:"packages"`
				Theme     string   `json:"theme"`
				Unmanaged bool     `json:"unmanaged"`
			}
			if err := json.Unmarshal(content, &document); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(document.Packages, []string{test.after}) || !document.Unmanaged || test.native && document.Theme != "handoff-native" {
				t.Fatalf("settings not preserved: %s", content)
			}
			retry, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
			if err != nil {
				t.Fatal(err)
			}
			defer retry.Close()
			if _, pending := retry.Reconciliation.CarrierHandoff(); pending {
				t.Fatal("completed source handoff was planned again")
			}
			for _, action := range retry.Reconciliation.Relations() {
				if action.CarrierIdentity().Carrier().Family() == desiredextension.CarrierPiPackage && action.InvokesHostRoute() {
					t.Fatal("completed source was reinstalled")
				}
			}
		})
	}
}

func TestGlobalAlreadyAbsentHandoffKeepsUnrelatedRetirementInFinal(t *testing.T) {
	fixture := newPiPinApplyFixture(t, target.ScopeGlobal)
	otherEnvironment := applyCarrierEnvironment(t, "unrelated", desiredextension.CarrierPiPackage, target.TargetPi, target.ScopeGlobal, desiredextension.SourceKindHostSource, "npm:unrelated@1")
	other, err := lockbuild.BuildWithOptions(t.Context(), otherEnvironment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatal(err)
	}
	seedApplyCarrierClaim(t, fixture.root, fixture.manifest, other, target.ScopeGlobal)
	fixture.after = "npm:replacement-tools@1"
	fixture.locked = piPinApplyLock(t, fixture.scope, fixture.after)
	writeApplyFile(t, fixture.manifest, fmt.Sprintf("version=1\ntargets=['pi']\n[[extension]]\nid='tools-managed'\ncarrier='pi-package'\nscope='global'\nsource={host_source=%q}\n", fixture.after))
	writeApplyLockfile(t, fixture.lockfile, fixture.locked)
	writeApplyFile(t, fixture.settings, `{"packages":[]}`)
	prepared, err := PlanWrite(t.Context(), fixture.input())
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	expectation, present := prepared.Reconciliation.CarrierHandoff()
	if !present || len(expectation.Handoffs().Retirements()) != 1 || len(expectation.After().CarrierAbsences()) != 1 {
		t.Fatal("unrelated retirement was moved into the handoff prerequisite")
	}
	calls := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
		calls++
		if !slices.Equal(request.Args, []string{"install", fixture.after}) || len(loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, fixture.scope)) != 0 {
			t.Fatal("already-absent claims did not settle before installation")
		}
		writeApplyFile(t, fixture.settings, fmt.Sprintf(`{"packages":[%q]}`, fixture.after))
		return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
	}})
	result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{HostRouteExecutor: executor})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result.CarrierHandoffProgress == nil || len(result.CarrierHandoffProgress.RetiredClaims) != 1 || !result.CarrierHandoffProgress.Complete {
		t.Fatal("split global settlements failed to complete")
	}
}
