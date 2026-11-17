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

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestOpenCodeHandoffRetiresBothDocumentsAndKeepsForeignRows(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			if native && runtime.GOOS == "windows" {
				t.Skip("fixture uses a POSIX shell host executable")
			}
			root := newApplyCarrierFixtureRoot(t)
			manifest := filepath.Join(root, "daem.toml")
			declaration := func(version string) string {
				return fmt.Sprintf("version=1\ntargets=['opencode']\n[[extension]]\nid='alpha'\ncarrier='opencode-plugin'\nscope='project'\nsource={host_source=%q}\n[[extension]]\nid='beta'\ncarrier='opencode-plugin'\nscope='project'\nsource={host_source=%q}\n", "alpha@"+version, "beta@"+version)
			}
			writeApplyFile(t, manifest, declaration("1"))
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
			writeApplyStatefile(t, filepath.Join(root, ".daem", "state.json"), applyStateSnapshot(t, durable.SnapshotInput{ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{first, second}}))
			server := filepath.Join(root, ".opencode", "opencode.json")
			tui := filepath.Join(root, ".opencode", "tui.jsonc")
			writeApplyFile(t, server, `{"plugin":["alpha@1",["foreign@1",{"autoload":false}],"beta@1"],"unmanaged":"keep"}`)
			writeApplyFile(t, tui, "// handoff canary\n"+`{"plugin":["alpha@1",["foreign@1",{"autoload":false}],"beta@1"],"unmanaged":"keep"}`)
			writeApplyFile(t, manifest, declaration("2"))
			if native {
				writeApplyFile(t, filepath.Join(root, "opencode.json"), `{"plugin":["beta@2","alpha@2","beta@1","alpha@1","root-only@1"]}`)
				writeApplyFile(t, filepath.Join(root, "preferences.json"), `{"model":"handoff-native"}`)
				writeApplyFile(t, manifest, declaration("2")+"\n[[host_config]]\nid='preferences'\npath='opencode.json'\nsource='./preferences.json'\nownership='keys'\n")
			}
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
				t.Fatal(err)
			}
			prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			calls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, command subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				if command.Command != "opencode" || len(command.Args) != 2 || command.Args[0] != "plugin" || calls > 2 {
					t.Fatalf("unexpected command: %s %v", command.Command, command.Args)
				}
				for _, path := range []string{server, tui} {
					content, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(content), "alpha@1") || strings.Contains(string(content), "beta@1") {
						t.Fatal("installation began before all selected documents were retired")
					}
				}
				content, err := os.ReadFile(server)
				if err != nil {
					t.Fatal(err)
				}
				var document struct {
					Plugin    []json.RawMessage `json:"plugin"`
					Unmanaged string            `json:"unmanaged"`
				}
				if err := json.Unmarshal(content, &document); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(command.Args[1])
				if err != nil {
					t.Fatal(err)
				}
				document.Plugin = append(document.Plugin, encoded)
				content, err = json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				writeApplyFile(t, server, string(content))
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			if native {
				binary := filepath.Join(root, "bin", "opencode")
				script := fmt.Sprintf(`#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.1.0\n'; exit 0; fi
[ "$1" = plugin ] || exit 91
grep -q handoff-native %q || exit 92
! grep -F -q -e 'alpha@1' -e 'beta@1' %q %q || exit 93
printf '%%s\n' "$2" >> %q
case "$2" in
alpha@2) printf '%%s\n' '{"plugin":[["foreign@1",{"autoload":false}],"alpha@2"],"unmanaged":"keep"}' > %q ;;
beta@2) printf '%%s\n' '{"plugin":[["foreign@1",{"autoload":false}],"beta@2","alpha@2"],"unmanaged":"keep"}' > %q ;;
*) exit 94 ;;
esac
`, filepath.Join(root, "opencode.json"), server, tui, filepath.Join(root, "host-calls"), server, server)
				writeApplyFile(t, binary, script)
				if err := os.Chmod(binary, 0o700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
				executor = subprocess.NewCommandExecutor(subprocess.CommandOptions{})
			}
			result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{HostRouteExecutor: executor})
			if err != nil {
				for _, attempt := range result.HostRouteAttempts {
					code, known := attempt.ExitCode()
					t.Logf("host exit=%d known=%t", code, known)
				}
				t.Fatal(err)
			}
			if native {
				content, err := os.ReadFile(filepath.Join(root, "host-calls"))
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != "alpha@2\nbeta@2\n" {
					t.Fatalf("unexpected native commands: %s", content)
				}
				calls = 2
				content, err = os.ReadFile(server)
				if err != nil {
					t.Fatal(err)
				}
				var document struct {
					Plugin []json.RawMessage `json:"plugin"`
				}
				if err := json.Unmarshal(content, &document); err != nil {
					t.Fatal(err)
				}
				if len(document.Plugin) != 3 || string(document.Plugin[1]) != `"alpha@2"` || string(document.Plugin[2]) != `"beta@2"` {
					t.Fatalf("post-retirement order did not converge: %s", content)
				}
				content, err = os.ReadFile(filepath.Join(root, "opencode.json"))
				if err != nil {
					t.Fatal(err)
				}
				var config struct {
					Model  string   `json:"model"`
					Plugin []string `json:"plugin"`
				}
				if err := json.Unmarshal(content, &config); err != nil {
					t.Fatal(err)
				}
				if config.Model != "handoff-native" || !slices.Equal(config.Plugin, []string{"beta@2", "alpha@2", "beta@1", "alpha@1", "root-only@1"}) {
					t.Fatal("protected native configuration was lost")
				}
			}
			if calls != 2 || len(result.HostRouteAttempts) != 2 {
				t.Fatal("OpenCode did not use two direct removals followed by two installs")
			}
			for _, path := range []string{server, tui} {
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(content), "foreign@1") || !strings.Contains(string(content), `"autoload":false`) || !strings.Contains(string(content), "keep") {
					t.Fatalf("unmanaged data lost: %s", content)
				}
				if path == tui && !strings.Contains(string(content), "// handoff canary") {
					t.Fatal("direct retirement discarded the JSONC comment")
				}
			}
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			if len(state.ManagedCarrierClaims()) != 2 || len(state.PendingCarrierRemovals()) != 0 || len(state.PendingCarrierInstalls()) != 0 {
				t.Fatal("OpenCode handoff did not settle its durable facts")
			}
		})
	}
}
