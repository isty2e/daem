package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestExecuteProviderReplayOwnsPendingCompletion(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, committed := range []bool{false, true} {
			name := string(scope) + "/first-install"
			if committed {
				name = string(scope) + "/reinstall"
			}
			t.Run(name, func(t *testing.T) {
				var root, agentRoot, manifestPath string
				if scope == target.ScopeGlobal {
					root, agentRoot, manifestPath = writeGlobalPiProviderMCPFixture(t)
				} else {
					root, manifestPath = writePiProviderMCPFixture(t)
					agentRoot = filepath.Join(root, ".pi")
				}
				packagePath := filepath.Join(agentRoot, "npm", "node_modules", "pi-mcp-adapter")
				configPath := filepath.Join(agentRoot, "mcp.json")
				statePath := filepath.Join(root, ".daem", "state.json")
				wantArgs := []string{"install", piProviderSource}
				if scope == target.ScopeProject {
					wantArgs = append(wantArgs, "-l")
				}
				calls := 0
				var cancelInstall context.CancelFunc
				options := ExecuteOptions{
					HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
						Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
							calls++
							if request.Command != "pi" || !slices.Equal(request.Args, wantArgs) {
								t.Fatalf("provider request = %#v", request)
							}
							writeApplyFile(t, filepath.Join(agentRoot, "settings.json"), `{"packages":["`+piProviderSource+`"]}`)
							writeApplyFile(t, filepath.Join(packagePath, "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0"}`)
							if cancelInstall != nil {
								cancelInstall()
							}
							return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
						},
					}),
				}
				var beforeConfig []byte
				var beforeInfo os.FileInfo
				if committed {
					initial, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := ExecuteWithOptions(t.Context(), initial, options); err != nil || calls != 1 {
						t.Fatalf("initial install calls=%d error=%v", calls, err)
					}
					beforeConfig, err = os.ReadFile(configPath)
					if err != nil {
						t.Fatal(err)
					}
					beforeInfo, err = os.Stat(configPath)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.RemoveAll(packagePath); err != nil {
						t.Fatal(err)
					}
				}

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				cancelInstall = cancel
				calls = 0
				interrupted, err := PlanWrite(ctx, CommandInput{ManifestPath: manifestPath})
				if err != nil {
					t.Fatal(err)
				}
				_, err = ExecuteWithOptions(ctx, interrupted, options)
				if !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatalf("interrupted install calls=%d error=%v", calls, err)
				}
				pending := loadApplyStatefile(t, statePath).PendingCarrierInstalls()
				if len(pending) != 1 {
					t.Fatalf("pending installs=%#v, want one", pending)
				}
				if err := os.RemoveAll(packagePath); err != nil {
					t.Fatal(err)
				}

				retry, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
				if err != nil {
					t.Fatal(err)
				}
				relations := retry.Reconciliation.Relations()
				providers := retry.lifecycle.planned.assessment.MCPProviders
				if len(relations) != 1 || relations[0].Kind() != reconcile.ActionNoOp ||
					len(providers) != 1 || providers[0].State() != readiness.MCPProviderInstallRequired ||
					providers[0].Reason() != readiness.MCPProviderReasonPackageAbsent {
					t.Fatalf("retry did not admit relation/settings-present, artifact-absent replay: relations=%#v providers=%#v", relations, providers)
				}
				replayed, present, err := providers[0].InstallAction()
				if err != nil || !present || replayed.Kind() != reconcile.ActionCreate ||
					!replayed.RouteRequest().Equal(pending[0].InstallRequest()) {
					t.Fatalf("InstallAction replay=%#v present=%t error=%v", replayed, present, err)
				}
				original := retry.lifecycle.planned.assessment.CurrentState
				for _, actions := range [][]reconcile.RelationAction{nil, relations, {replayed}, {replayed, replayed}} {
					projected, err := postProviderPlanningState(original, actions)
					wantPending := 1
					if len(actions) != 0 && actions[0].InvokesHostRoute() {
						wantPending = 0
					}
					if err != nil || len(projected.PendingCarrierInstalls()) != wantPending ||
						len(original.PendingCarrierInstalls()) != 1 || !original.PendingCarrierInstalls()[0].ExactEqual(pending[0]) {
						t.Fatalf("planning projection mutated or misassigned pending: projected=%#v original=%#v error=%v", projected.PendingCarrierInstalls(), original.PendingCarrierInstalls(), err)
					}
				}
				request := pending[0].InstallRequest()
				otherRequest, err := realizationdelegate.NewRequest(request.RouteID(), request.ContractVersion()+"-other", request.CanonicalRequestHash())
				if err != nil {
					t.Fatal(err)
				}
				otherPending, err := durablecarrier.NewPendingCarrierInstall(pending[0].Owner(), pending[0].Identity(), otherRequest)
				if err != nil {
					t.Fatal(err)
				}
				unmatched, err := original.WithPendingCarrierInstalls([]durablecarrier.PendingCarrierInstall{otherPending})
				if committed && scope == target.ScopeProject {
					if err == nil {
						t.Fatal("snapshot accepted pending request contradicting its managed claim")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					projected, err := postProviderPlanningState(unmatched, []reconcile.RelationAction{replayed})
					if err != nil || len(projected.PendingCarrierInstalls()) != 1 || !projected.PendingCarrierInstalls()[0].ExactEqual(otherPending) {
						t.Fatalf("projection consumed a different request: pending=%#v error=%v", projected.PendingCarrierInstalls(), err)
					}
				}
				if err := retry.Close(); err != nil {
					t.Fatal(err)
				}
				replayCtx, replayCancel := context.WithCancel(t.Context())
				defer replayCancel()
				canceledReplay, err := PlanWrite(replayCtx, CommandInput{ManifestPath: manifestPath})
				if err != nil {
					t.Fatal(err)
				}
				calls = 0
				cancelInstall = replayCancel
				_, err = ExecuteWithOptions(replayCtx, canceledReplay, options)
				stillPending := loadApplyStatefile(t, statePath).PendingCarrierInstalls()
				if !errors.Is(err, context.Canceled) || calls != 1 || len(stillPending) != 1 || !stillPending[0].ExactEqual(pending[0]) {
					t.Fatalf("canceled replay calls=%d pending=%#v error=%v", calls, stillPending, err)
				}
				if err := os.RemoveAll(packagePath); err != nil {
					t.Fatal(err)
				}
				retry, err = PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
				if err != nil {
					t.Fatal(err)
				}
				calls = 0
				cancelInstall = nil
				result, executionErr := ExecuteWithOptions(t.Context(), retry, options)
				settled := loadApplyStatefile(t, statePath)
				t.Logf("replay calls=%d attempted=%t pending_after=%d error=%v", calls, result.ExecutionAttempted, len(settled.PendingCarrierInstalls()), executionErr)
				if executionErr != nil {
					t.Fatalf("provider replay failed after settlement: %v", executionErr)
				}
				if calls != 1 || len(result.HostRouteAttempts) != 1 || !result.ExecutionAttempted || len(settled.PendingCarrierInstalls()) != 0 {
					t.Fatalf("replay result=%#v pending=%#v calls=%d", result, settled.PendingCarrierInstalls(), calls)
				}
				claims := settled.ManagedCarrierClaims()
				if scope == target.ScopeGlobal {
					store, err := carrierclaimstore.New(isolatedApplyCarrierRegistryPath(t, root))
					if err != nil {
						t.Fatal(err)
					}
					registry, err := store.Load(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					claims = registry.Claims()
				}
				if len(claims) != 1 || !claims[0].Owner().ExactEqual(pending[0].Owner()) ||
					!claims[0].Identity().ExactEqual(pending[0].Identity()) ||
					!claims[0].InstallRequest().Equal(pending[0].InstallRequest()) ||
					claims[0].Provenance() != durablecarrier.ClaimProvenanceInstalledObserved {
					t.Fatalf("settled claims=%#v, want exact installed-observed claim", claims)
				}
				afterConfig, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if committed {
					afterInfo, err := os.Stat(configPath)
					if err != nil || string(beforeConfig) != string(afterConfig) ||
						!os.SameFile(beforeInfo, afterInfo) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
						t.Fatalf("converged config changed: %v", err)
					}
				}
				settledBytes, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				subsequent, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
				if err != nil {
					t.Fatal(err)
				}
				calls = 0
				result, err = ExecuteWithOptions(t.Context(), subsequent, options)
				if err != nil || result.ExecutionAttempted || calls != 0 {
					t.Fatalf("subsequent execution=%t calls=%d error=%v", result.ExecutionAttempted, calls, err)
				}
				for path, before := range map[string][]byte{statePath: settledBytes, configPath: afterConfig} {
					if after, err := os.ReadFile(path); err != nil || string(before) != string(after) {
						t.Fatalf("subsequent execution changed %q: %v", path, err)
					}
				}
			})
		}
	}
}

func TestExecuteProviderReplayPreservesOtherPendingCompletion(t *testing.T) {
	for _, replayScope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(replayScope), func(t *testing.T) {
			root, globalRoot, manifestPath := writeGlobalPiProviderMCPFixture(t)
			projectRoot := filepath.Join(root, ".pi")
			manifest, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			writeApplyFile(t, manifestPath, string(manifest)+`
[[extension]]
id = "pi-mcp-adapter-project"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }

[[mcp_server]]
name = "project-context"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["project.js"]
`)
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			var cancelInstall context.CancelFunc
			wantScope := target.ScopeGlobal
			options := ExecuteOptions{
				HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
					Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
						calls++
						wantArgs := []string{"install", piProviderSource}
						agentRoot := globalRoot
						if wantScope == target.ScopeProject {
							wantArgs = append(wantArgs, "-l")
							agentRoot = projectRoot
						}
						if request.Command != "pi" || !slices.Equal(request.Args, wantArgs) {
							t.Fatalf("unexpected provider request=%#v, want %s", request, wantScope)
						}
						writeApplyFile(t, filepath.Join(agentRoot, "settings.json"), `{"packages":["`+piProviderSource+`"]}`)
						writeApplyFile(t, filepath.Join(agentRoot, "npm", "node_modules", "pi-mcp-adapter", "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0"}`)
						if cancelInstall != nil {
							cancelInstall()
						}
						return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
					},
				}),
			}
			statePath := filepath.Join(root, ".daem", "state.json")
			for index, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				wantScope = scope
				cancelInstall = cancel
				calls = 0
				prepared, err := PlanWrite(ctx, CommandInput{ManifestPath: manifestPath})
				if err != nil {
					t.Fatal(err)
				}
				_, err = ExecuteWithOptions(ctx, prepared, options)
				if !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatalf("interrupted %s install calls=%d error=%v", scope, calls, err)
				}
				if pending := loadApplyStatefile(t, statePath).PendingCarrierInstalls(); len(pending) != index+1 {
					t.Fatalf("pending after %s install=%#v", scope, pending)
				}
			}
			missingRoot := globalRoot
			if replayScope == target.ScopeProject {
				missingRoot = projectRoot
			}
			if err := os.RemoveAll(filepath.Join(missingRoot, "npm", "node_modules", "pi-mcp-adapter")); err != nil {
				t.Fatal(err)
			}
			retry, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
			if err != nil {
				t.Fatal(err)
			}
			planned := retry.lifecycle.planned
			actions, err := providerInstallActions(planned.assessment.MCPProviders)
			if err != nil || len(actions) != 1 || actions[0].Scope() != replayScope {
				t.Fatalf("provider replay actions=%#v error=%v", actions, err)
			}
			projected, err := postProviderPlanningState(planned.assessment.CurrentState, actions)
			remaining := projected.PendingCarrierInstalls()
			if err != nil || len(remaining) != 1 || remaining[0].Identity().Scope() == replayScope ||
				len(planned.assessment.CurrentState.PendingCarrierInstalls()) != 2 {
				t.Fatalf("projection lost other owner's pending: remaining=%#v error=%v", remaining, err)
			}
			calls = 0
			cancelInstall = nil
			wantScope = replayScope
			result, err := ExecuteWithOptions(t.Context(), retry, options)
			if err != nil || calls != 1 || !result.ExecutionAttempted {
				t.Fatalf("mixed replay calls=%d attempted=%t error=%v", calls, result.ExecutionAttempted, err)
			}
			settled := loadApplyStatefile(t, statePath)
			if len(settled.PendingCarrierInstalls()) != 0 || len(settled.ManagedCarrierClaims()) != 1 {
				t.Fatalf("mixed pending=%#v project claims=%#v", settled.PendingCarrierInstalls(), settled.ManagedCarrierClaims())
			}
			store, err := carrierclaimstore.New(isolatedApplyCarrierRegistryPath(t, root))
			if err != nil {
				t.Fatal(err)
			}
			registry, err := store.Load(t.Context())
			if err != nil || len(registry.Claims()) != 1 {
				t.Fatalf("mixed global registry=%#v error=%v", registry.Claims(), err)
			}
			for _, pending := range planned.assessment.CurrentState.PendingCarrierInstalls() {
				claim := registry.Claims()[0]
				if pending.Identity().Scope() == target.ScopeProject {
					claim = settled.ManagedCarrierClaims()[0]
				}
				if !claim.Owner().ExactEqual(pending.Owner()) || !claim.Identity().ExactEqual(pending.Identity()) ||
					!claim.InstallRequest().Equal(pending.InstallRequest()) || claim.Provenance() != durablecarrier.ClaimProvenanceInstalledObserved {
					t.Fatalf("mixed claim=%#v differs from pending=%#v", claim, pending)
				}
			}
			settledFiles := make(map[string][]byte)
			for _, path := range []string{statePath, isolatedApplyCarrierRegistryPath(t, root), filepath.Join(globalRoot, "mcp.json"), filepath.Join(projectRoot, "mcp.json")} {
				settledFiles[path], err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			subsequent, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
			if err != nil {
				t.Fatal(err)
			}
			calls = 0
			result, err = ExecuteWithOptions(t.Context(), subsequent, options)
			if err != nil || calls != 0 || result.ExecutionAttempted {
				t.Fatalf("subsequent mixed execution=%t calls=%d error=%v", result.ExecutionAttempted, calls, err)
			}
			for path, before := range settledFiles {
				if after, err := os.ReadFile(path); err != nil || string(before) != string(after) {
					t.Fatalf("subsequent mixed execution changed %q: %v", path, err)
				}
			}
		})
	}
}
