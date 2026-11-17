package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestCarrierSourceSwapRetiresTheWholeSetBeforeAcquisition(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			root := newApplyCarrierFixtureRoot(t)
			manifest := filepath.Join(root, "daem.toml")
			inventory := filepath.Join(root, "claude", "plugins", "installed_plugins.json")
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Dir(filepath.Dir(inventory)))
			declaration := func(a, b string) string {
				return fmt.Sprintf("version=1\ntargets=['claude-code']\n[[extension]]\nid='a'\ncarrier='claude-code-plugin'\nscope='project'\nsource={marketplace=%q}\n[[extension]]\nid='b'\ncarrier='claude-code-plugin'\nscope='project'\nsource={marketplace=%q}\n", a, b)
			}
			writeApplyFile(t, manifest, declaration("x@market", "y@market"))
			old, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest})
			if err != nil {
				t.Fatal(err)
			}
			first := seedApplyCarrierClaim(t, root, manifest, old.Lockfile, target.ScopeProject)
			record := old.Lockfile.Locked.Subjects()[1]
			identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
			if err != nil || !admitted {
				t.Fatal(err)
			}
			request, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
			if err != nil {
				t.Fatal(err)
			}
			second, err := durablecarrier.NewManagedCarrierClaim(first.Owner(), identity, request, durablecarrier.ClaimProvenanceInstalledObserved)
			if err != nil {
				t.Fatal(err)
			}
			oldClaims := []durablecarrier.ManagedCarrierClaim{first, second}
			writeApplyStatefile(t, filepath.Join(root, ".daem", "state.json"), applyStateSnapshot(t, durable.SnapshotInput{ManagedCarrierClaims: oldClaims}))
			current := map[string]bool{"x@market": true, "y@market": true}
			writeInventory := func() {
				plugins := make(map[string]json.RawMessage)
				for source := range current {
					plugins[source] = json.RawMessage(fmt.Sprintf(`[{"scope":"project","projectPath":%q}]`, root))
				}
				content, err := json.Marshal(struct {
					Version int                        `json:"version"`
					Plugins map[string]json.RawMessage `json:"plugins"`
				}{2, plugins})
				if err != nil {
					t.Fatal(err)
				}
				writeApplyFile(t, inventory, string(content))
			}
			writeInventory()
			writeApplyFile(t, manifest, declaration("y@market", "x@market"))
			final, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest})
			if err != nil {
				t.Fatal(err)
			}
			failed, installs := false, 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, command subprocess.CommandRequest) subprocess.CommandResult {
				if len(command.Args) < 3 {
					t.Fatal("short Claude command")
				}
				verb, source := command.Args[1], command.Args[2]
				switch verb {
				case "uninstall":
					if installs != 0 {
						t.Fatal("retirement continued after an acquisition")
					}
					if partial && !failed && source == "y@market" {
						failed = true
						return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 23}
					}
					delete(current, source)
				case "install":
					state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
					for _, claim := range state.ManagedCarrierClaims() {
						for _, oldClaim := range oldClaims {
							if claim.ExactEqual(oldClaim) {
								t.Fatal("swap acquired a source before every old claim settled")
							}
						}
					}
					installs++
					current[source] = true
				default:
					t.Fatalf("unexpected Claude command %v", command.Args)
				}
				writeInventory()
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			apply := func() error {
				prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
				if err != nil {
					return err
				}
				defer prepared.Close()
				result, executeErr := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{HostRouteExecutor: executor})
				if executeErr != nil && partial {
					if result.CarrierHandoffProgress == nil || len(result.CarrierHandoffProgress.RetiredClaims) != 1 || result.CarrierHandoffProgress.Requalified || result.CarrierHandoffProgress.Complete {
						t.Fatal("partial swap did not report its completed retirement")
					}
				}
				return executeErr
			}
			err = apply()
			if partial {
				if err == nil || installs != 0 {
					t.Fatal("partial set retirement proceeded into swap acquisitions")
				}
				err = apply()
			}
			if err != nil {
				t.Fatal(err)
			}
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			if installs != 2 || len(state.ManagedCarrierClaims()) != 2 || len(state.PendingCarrierRemovals()) != 0 || len(state.PendingCarrierInstalls()) != 0 {
				t.Fatal("same-manifest swap failed to converge")
			}
			for _, record := range final.Lockfile.Locked.Subjects() {
				matched := false
				for _, claim := range state.ManagedCarrierClaims() {
					matched = matched || claim.MatchesLockedRecord(record)
				}
				if !matched {
					t.Fatalf("swap lost final subject %s", record.SubjectID())
				}
			}
		})
	}
}
