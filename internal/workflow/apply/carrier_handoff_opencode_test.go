package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{HostRouteExecutor: executor})
	if err != nil {
		t.Fatal(err)
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
}
