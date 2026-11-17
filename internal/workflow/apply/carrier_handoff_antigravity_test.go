package apply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	observeantigravity "github.com/isty2e/daem/internal/assurance/observe/antigravityplugin"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestAntigravitySourceHandoffPreservesEndpointAssurance(t *testing.T) {
	for _, selector := range []bool{true, false} {
		t.Run(fmt.Sprint(selector), func(t *testing.T) {
			root, manifest, lockfile, _, old, _ := writeApplyAntigravityCLIPluginCarrierCommandFixture(t)
			t.Setenv("HOME", filepath.Join(root, "home"))
			seedApplyCarrierClaim(t, root, manifest, old, target.ScopeGlobal)
			paths, err := observeantigravity.ResolveHostPaths()
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := paths.PluginDirectoryPath("modern-web-guidance")
			if err != nil {
				t.Fatal(err)
			}
			writeApplyFile(t, paths.ImportManifestPath(), `{"imports":[{"name":"modern-web-guidance","source":"antigravity"}]}`)
			writeApplyFile(t, filepath.Join(bundle, "plugin.json"), `{"name":"modern-web-guidance","version":"1.0.0"}`)
			after := "modern-web-guidance@community"
			if !selector {
				after = "https://example.invalid/plugins/guidance.zip"
			}
			writeApplyFile(t, manifest, fmt.Sprintf("version=1\ntargets=['antigravity-cli']\n[[extension]]\nid='guidance-managed'\ncarrier='antigravity-cli-plugin'\nscope='global'\nsource={host_source=%q}\n", after))
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
				t.Fatal(err)
			}
			prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, LockfilePath: lockfile})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if _, present := prepared.Reconciliation.CarrierHandoff(); !present || prepared.Reconciliation.HasErrors() {
				t.Fatal("Antigravity handoff was not conditionally admitted")
			}
			calls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				if calls == 1 {
					if !slices.Equal(request.Args, []string{"plugin", "uninstall", "modern-web-guidance"}) {
						t.Fatalf("not a removal: %s %v", request.Command, request.Args)
					}
					writeApplyFile(t, paths.ImportManifestPath(), `{"imports":[]}`)
					if err := os.RemoveAll(bundle); err != nil {
						t.Fatal(err)
					}
				} else {
					if calls != 2 || !slices.Contains(request.Args, after) || len(loadCarrierClaimsForScope(t, root, manifest, target.ScopeGlobal)) != 0 {
						t.Fatal("Antigravity install preceded retirement")
					}
					if selector {
						writeApplyFile(t, paths.ImportManifestPath(), `{"imports":[{"name":"modern-web-guidance","source":"antigravity"}]}`)
						writeApplyFile(t, filepath.Join(bundle, "plugin.json"), `{"name":"modern-web-guidance","version":"1.0.1"}`)
					}
				}
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{HostRouteExecutor: executor})
			if err != nil {
				t.Fatal(err)
			}
			claims := loadCarrierClaimsForScope(t, root, manifest, target.ScopeGlobal)
			wantClaims := 0
			if selector {
				wantClaims = 1
			}
			if calls != 2 || len(claims) != wantClaims || len(result.HostRouteAttempts) != 2 {
				t.Fatal("Antigravity endpoint assurance was strengthened or lost")
			}
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			if len(state.PendingCarrierRemovals()) != 0 || len(state.PendingCarrierInstalls()) != 0 {
				t.Fatal("settled Antigravity handoff left pending debt")
			}
		})
	}
}
