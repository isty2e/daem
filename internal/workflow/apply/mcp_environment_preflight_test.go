package apply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/subprocess"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestMCPEnvironmentPreflightDryRunDoesNotRequireRuntimePresence(t *testing.T) {
	paths := writeMCPPreflightWorkspace(t, false)
	result, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		TargetValues: []string{string(targetpkg.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("PlanDryRun returned error: %v", err)
	}
	if !result.ReconciliationReady {
		t.Fatal("dry-run reconciliation is not ready")
	}
}

func TestPlanWriteRejectsMissingMCPEnvironmentBeforePreparingMutation(t *testing.T) {
	paths := writeMCPPreflightWorkspace(t, false)
	lookedUp := make([]string, 0)

	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		TargetValues: []string{string(targetpkg.TargetClaudeCode)},
		environmentPresent: func(name string) bool {
			lookedUp = append(lookedUp, name)
			return false
		},
	})
	if err == nil || !strings.Contains(err.Error(), "DAEM_MENV_TEST_SOURCE") {
		t.Fatalf("PlanWrite error = %v, want missing source", err)
	}
	if closeErr := prepared.Close(); closeErr != nil {
		t.Fatalf("close unavailable plan: %v", closeErr)
	}
	if !slices.Equal(lookedUp, []string{"DAEM_MENV_TEST_SOURCE"}) {
		t.Fatalf("presence lookups = %#v", lookedUp)
	}
	assertMCPPreflightOutputsAbsent(t, paths)
}

func TestExecuteRechecksMCPEnvironmentBeforeAnyMixedTargetMutation(t *testing.T) {
	paths := writeMCPPreflightWorkspace(t, true)
	present := true
	lookups := 0
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		TargetValues: []string{
			string(targetpkg.TargetClaudeCode),
			string(targetpkg.TargetCodex),
		},
		environmentPresent: func(name string) bool {
			lookups++
			return present && name == "DAEM_MENV_TEST_SOURCE"
		},
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	present = false

	_, err = ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{})
	if err == nil || !strings.Contains(err.Error(), "DAEM_MENV_TEST_SOURCE") {
		t.Fatalf("ExecuteWithOptions error = %v, want missing source", err)
	}
	if lookups != 2 {
		t.Fatalf("presence lookups = %d, want plan and pre-lease execution checks", lookups)
	}
	assertMCPPreflightOutputsAbsent(t, paths)
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(paths.ManifestPath), "AGENTS.md")); !os.IsNotExist(statErr) {
		t.Fatalf("mixed-target instruction destination stat error = %v, want absent", statErr)
	}
}

func TestExecuteRechecksMCPEnvironmentAfterLeaseProtectedFreshPlan(t *testing.T) {
	paths := writeMCPPreflightWorkspace(t, true)
	lookups := 0
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		TargetValues: []string{
			string(targetpkg.TargetClaudeCode),
			string(targetpkg.TargetCodex),
		},
		environmentPresent: func(name string) bool {
			lookups++
			return lookups < 3 && name == "DAEM_MENV_TEST_SOURCE"
		},
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}

	_, err = ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{})
	if err == nil || !strings.Contains(err.Error(), "DAEM_MENV_TEST_SOURCE") {
		t.Fatalf("ExecuteWithOptions error = %v, want fresh-plan missing source", err)
	}
	if lookups != 3 {
		t.Fatalf("presence lookups = %d, want plan, pre-lease, and post-replan checks", lookups)
	}
	assertMCPPreflightOutputsAbsent(t, paths)
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(paths.ManifestPath), "AGENTS.md")); !os.IsNotExist(statErr) {
		t.Fatalf("mixed-target instruction destination stat error = %v, want absent", statErr)
	}
}

func TestExecuteAcceptsEmptyMCPEnvironmentSource(t *testing.T) {
	const sourceName = "DAEM_MENV_TEST_SOURCE"
	t.Setenv(sourceName, "")
	paths := writeMCPPreflightWorkspace(t, false)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		TargetValues: []string{string(targetpkg.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	delegateRuns := 0
	result, err := ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				delegateRuns++
				if !slices.Contains(request.Env, "TOKEN=") {
					t.Fatalf("delegate env = %#v, want present empty TOKEN", request.Env)
				}
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if result.ActionCount != 1 || delegateRuns != 1 {
		t.Fatalf("result action_count=%d delegate_runs=%d, want one of each", result.ActionCount, delegateRuns)
	}
}

func TestPlanWriteChecksDeclaredMCPEnvironmentWhenProjectionIsCurrent(t *testing.T) {
	paths := writeMCPPreflightWorkspace(t, false)
	input := CommandInput{
		ManifestPath:       paths.ManifestPath,
		TargetValues:       []string{string(targetpkg.TargetClaudeCode)},
		environmentPresent: func(string) bool { return true },
	}
	prepared, err := PlanWrite(context.Background(), input)
	if err != nil {
		t.Fatalf("initial PlanWrite returned error: %v", err)
	}
	_, err = ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			LookupEnv: func(string) (string, bool) { return "", true },
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				return subprocess.CommandResult{}
			},
		}),
	})
	if err != nil {
		t.Fatalf("initial ExecuteWithOptions returned error: %v", err)
	}

	input.environmentPresent = func(string) bool { return false }
	retry, err := PlanWrite(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "DAEM_MENV_TEST_SOURCE") {
		t.Fatalf("no-op PlanWrite error = %v, want declared missing source", err)
	}
	if closeErr := retry.Close(); closeErr != nil {
		t.Fatalf("close unavailable retry: %v", closeErr)
	}
}

func TestStateOnlyMCPRemovalDoesNotRequireRemovedEnvironmentSource(t *testing.T) {
	paths := writeMCPPreflightWorkspace(t, false)
	initial, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:       paths.ManifestPath,
		TargetValues:       []string{string(targetpkg.TargetClaudeCode)},
		environmentPresent: func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("initial PlanWrite returned error: %v", err)
	}
	_, err = ExecuteWithOptions(context.Background(), initial, ExecuteOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			LookupEnv: func(string) (string, bool) { return "transient", true },
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	})
	if err != nil {
		t.Fatalf("initial ExecuteWithOptions returned error: %v", err)
	}

	writeApplyFile(t, paths.ManifestPath, `version = 1
targets = ["claude-code"]
`)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{
		ManifestPath: paths.ManifestPath,
	}); err != nil {
		t.Fatalf("remove RunLock returned error: %v", err)
	}
	lookups := 0
	removal, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		TargetValues: []string{string(targetpkg.TargetClaudeCode)},
		environmentPresent: func(string) bool {
			lookups++
			return false
		},
	})
	if err != nil {
		t.Fatalf("removal PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(context.Background(), removal, ExecuteOptions{})
	if err != nil {
		t.Fatalf("removal ExecuteWithOptions returned error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("removal action_count = %d, want one", result.ActionCount)
	}
	if lookups != 0 {
		t.Fatalf("removed environment source lookups = %d, want none", lookups)
	}
	assertApplyMCPConfigMissing(
		t,
		filepath.Join(filepath.Dir(paths.ManifestPath), aggregate.ClaudeProjectMCPConfigPath),
		"menv-test",
	)
	assertApplyMCPStateSubjectMissing(t, loadApplyStatefile(t, paths.StatefilePath), "menv-test")
}

func mcpPreflightEnvironment(
	t *testing.T,
	targets []targetpkg.Target,
	servers ...desiredmcp.Server,
) desired.Environment {
	t.Helper()
	return desiredtest.Environment(t, desired.Spec{
		Targets:    targets,
		Defaults:   desiredtest.Defaults(t, targetpkg.ScopeProject, skill.InstallModeCopy),
		MCPServers: servers,
	})
}

func mcpPreflightServer(
	t *testing.T,
	name string,
	selectedTarget targetpkg.Target,
	scope targetpkg.Scope,
	env map[string]string,
) desiredmcp.Server {
	t.Helper()
	references := make(map[string]desiredmcp.EnvReference, len(env))
	for childName, sourceName := range env {
		references[childName] = desiredtest.MCPEnvReference(t, sourceName)
	}
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		[]string{"-y", "@example/server@1.0.0"},
		references,
	)
	binding := desiredtest.MCPBinding(
		t,
		selectedTarget,
		scope,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	return desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     name,
		Bindings: []desiredmcp.Binding{binding},
	})
}

func mcpPreflightSelection(
	t *testing.T,
	available []targetpkg.Target,
	requested ...targetpkg.Target,
) targetselection.Selection {
	t.Helper()
	values := make([]string, 0, len(requested))
	for _, selected := range requested {
		values = append(values, string(selected))
	}
	selection, err := targetselection.ForAvailableTargets(available, values)
	if err != nil {
		t.Fatalf("build target selection: %v", err)
	}
	return selection
}

func writeMCPPreflightWorkspace(t *testing.T, includeInstruction bool) daempaths.Paths {
	t.Helper()
	root := t.TempDir()
	paths := applyTestPaths(t, root)
	targets := `["claude-code"]`
	instruction := ""
	if includeInstruction {
		targets = `["claude-code", "codex"]`
		writeApplyFile(t, filepath.Join(root, "instructions", "AGENTS.md"), "mixed target instructions\n")
		instruction = `
[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`
	}
	writeApplyFile(t, paths.ManifestPath, fmt.Sprintf(`version = 1
targets = %s
%s
[[mcp_server]]
name = "menv-test"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@example/server@1.0.0"]
env = { TOKEN = { from_env = "DAEM_MENV_TEST_SOURCE" } }
`, targets, instruction))
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{
		ManifestPath: paths.ManifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	return paths
}

func assertMCPPreflightOutputsAbsent(t *testing.T, paths daempaths.Paths) {
	t.Helper()
	root := filepath.Dir(paths.ManifestPath)
	for _, path := range []string{
		filepath.Join(root, ".mcp.json"),
		paths.StatefilePath,
		paths.RecoveryDir,
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s stat error = %v, want absent", path, err)
		}
	}
}
