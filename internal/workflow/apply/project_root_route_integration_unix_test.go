//go:build darwin || linux

package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/subprocess"
	targetpkg "github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestExecuteWithOptionsLaunchesDefaultHostRouteRunnerFromCapturedRoot(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, _, subject := writeApplyCodexPluginCarrierCommandFixture(t)
	if err := os.WriteFile(filepath.Join(root, "route-sentinel"), []byte("captured-root"), 0o600); err != nil {
		t.Fatalf("write route sentinel: %v", err)
	}
	binDir := t.TempDir()
	runnerPath := filepath.Join(binDir, "codex")
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\nset -eu\ncat route-sentinel > route-cwd-output\n"), 0o700); err != nil {
		t.Fatalf("write fake codex runner: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	planning, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"codex"},
		RelationObservations: &missingInventory,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	observer := func(context.Context, executehostroute.Command, []durablecarrier.PendingCarrierInstall, []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
		return assurancehostroute.CurrentObservation(observerelation.Correlate(subject.ExpectedRelation(), applyRelationInventory(t, observerelation.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observerelation.Row{
				applyRelationManagedRow(t, "documents@openai-primary-runtime", string(subject.ExpectedRelation().ManagedInstanceKey())),
			},
		})))
	}

	if _, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
		RelationObservations: &missingInventory,
		HostRouteExecutor:    subprocess.NewCommandExecutor(subprocess.CommandOptions{}),
		HostRouteObserver:    observer,
	}); err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "route-cwd-output"))
	if err != nil {
		t.Fatalf("read fake route cwd output: %v", err)
	}
	if string(payload) != "captured-root" {
		t.Fatalf("fake route cwd output = %q, want captured-root", payload)
	}
}

func TestExecuteWithOptionsLaunchesMCPDelegateFromCapturedRootWithMappedEnvironment(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(filepath.Join(root, "delegate-sentinel"), []byte("captured-root"), 0o600); err != nil {
		t.Fatalf("write delegate sentinel: %v", err)
	}
	binDir := t.TempDir()
	runnerPath := filepath.Join(binDir, "delegate-cwd-test")
	script := "#!/bin/sh\nset -eu\nprintf '%s:%s' \"$(cat delegate-sentinel)\" \"$API_TOKEN\" > delegate-cwd-output\n"
	if err := os.WriteFile(runnerPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake delegate runner: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DAEM_HOST_TOKEN", "mapped-secret")
	writeApplyFile(t, manifestPath, `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "cwd-check"
transport = "stdio"
command = "delegate-cwd-test"
env = { API_TOKEN = { from_env = "DAEM_HOST_TOKEN" } }
`)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	planning, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(targetpkg.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}

	if _, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{}),
	}); err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "delegate-cwd-output"))
	if err != nil {
		t.Fatalf("read fake delegate cwd output: %v", err)
	}
	if string(payload) != "captured-root:mapped-secret" {
		t.Fatalf("fake delegate output = %q, want captured root and mapped secret", payload)
	}
}

func TestExecuteWithOptionsRejectsProjectRootRetargetedDuringMCPDelegate(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyFile(t, manifestPath, `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "cwd-retarget"
transport = "stdio"
command = "delegate-retarget-test"
`)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	planning, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(targetpkg.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}

	moved := root + "-captured"
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	runnerCalled := false
	result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				runnerCalled = true
				if renameErr := os.Rename(root, moved); renameErr != nil {
					return subprocess.CommandResult{Started: true, Err: renameErr}
				}
				if mkdirErr := os.Mkdir(root, 0o700); mkdirErr != nil {
					return subprocess.CommandResult{Started: true, Err: mkdirErr}
				}
				return subprocess.CommandResult{Started: true, HasExitCode: true}
			},
		}),
	})

	if !runnerCalled {
		t.Fatal("delegate runner was not called")
	}
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ExecuteWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if len(result.DelegateAttempts) != 1 ||
		result.DelegateAttempts[0].Attempt().Reason() != delegate.ReasonWorkDirAuthority {
		t.Fatalf("delegate attempts = %#v, want one workdir authority failure", result.DelegateAttempts)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".daem", "state.json")); !os.IsNotExist(statErr) {
		t.Fatalf("replacement-root statefile stat error = %v, want absent", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(moved, ".daem", "state.json")); statErr != nil {
		t.Fatalf("captured-root statefile stat error = %v, want retained state", statErr)
	}
	state := loadApplyStatefile(t, filepath.Join(moved, ".daem", "state.json"))
	if len(state.DelegateAttempts()) != 0 {
		t.Fatalf("persisted delegate attempts = %#v, want none after root retarget", state.DelegateAttempts())
	}
}
