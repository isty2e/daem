package apply

import (
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	mcptest "github.com/isty2e/daem/test/testkit/mcp"
)

func compareApplyMCPPlacementCanonicalEntry(
	t *testing.T,
	id aggregate.MCPPlacementID,
	content []byte,
	serverID string,
	canonical []byte,
) (mcpcodec.MCPProjectionCanonicalComparison, error) {
	t.Helper()
	operations, ok := mcptest.OperationsForPlacementID(id)
	if !ok {
		t.Fatalf("MCP placement operations %q missing", id)
	}
	return mcptest.CompareCanonicalEntry(operations, content, serverID, canonical)
}

func applyDelegateRunOptions(t *testing.T, paths daempaths.Paths, options runOptions) runOptions {
	t.Helper()
	root, err := rootedpath.CaptureRoot(paths.ManifestRoot)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close apply delegate root: %v", err)
		}
	})
	options.projectRoot = root
	if !options.executionGuard.valid {
		options.executionGuard = testApplyExecutionGuard(t, paths)
	}
	return options
}

func requireApplyMCPAggregateDecision(
	t *testing.T,
	result reconcile.Result,
	serverID string,
) reconcile.AggregateSubjectDecision {
	t.Helper()
	for _, decision := range result.Decisions() {
		aggregateDecision, ok := decision.Aggregate()
		if !ok {
			continue
		}
		name, isMCP := topologymcp.ServerID(aggregateDecision.Subject())
		if isMCP && name == serverID {
			return aggregateDecision
		}
	}
	t.Fatalf("plan decisions = %#v, want MCP projection subject %q", result.Decisions(), serverID)
	return reconcile.AggregateSubjectDecision{}
}

func subjectHasMCPPlacement(subject topology.SubjectID, placementID aggregate.MCPPlacementID) bool {
	placement, ok := aggregate.MCPPlacementForSubject(subject)
	return ok && placement.ID() == placementID
}

func applyMCPSelection(t *testing.T) targetselection.Selection {
	t.Helper()
	selection, err := targetselection.ForAvailableTargets([]targetpkg.Target{targetpkg.TargetClaudeCode}, []string{string(targetpkg.TargetClaudeCode)})
	if err != nil {
		t.Fatalf("build MCP selection: %v", err)
	}
	return selection
}

func applySelectedTargets(t *testing.T, selection targetselection.Selection) reconcile.SelectedTargets {
	t.Helper()
	selected, err := reconcile.NewSelectedTargets(selection.Targets())
	if err != nil {
		t.Fatalf("normalize selected targets: %v", err)
	}
	return selected
}

func applyMCPLockfile(t *testing.T, serverID string, command string, args []string) (lock.File, []byte) {
	t.Helper()
	env := map[string]string{"API_TOKEN": "${DAEM_TEST_TOKEN}"}
	projection := mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            args,
		Env:             env,
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	}
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	server, binding := applyMCPStdioServer(
		t,
		serverID,
		targetpkg.TargetClaudeCode,
		command,
		args,
		map[string]string{"API_TOKEN": "DAEM_TEST_TOKEN"},
	)
	graph, err := topologymcp.Binding(server, binding)
	if err != nil {
		t.Fatalf("MCPServer returned error: %v", err)
	}
	delegatePlan, err := mcpdelegate.MCPBindingDelegatePlan(server, binding)
	if err != nil {
		t.Fatalf("MCPServerDelegatePlan returned error: %v", err)
	}
	placement, ok := aggregate.ImplementedMCPPlacement(targetpkg.TargetClaudeCode, targetpkg.ScopeProject)
	if !ok {
		t.Fatal("Claude project MCP placement is unavailable")
	}
	record, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:                graph,
		EntityID:             server.ID(),
		PlacementID:          placement.ID(),
		ServerID:             serverID,
		RequestedOnAbsent:    desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:      command,
		LauncherArgs:         args,
		CanonicalProjection:  string(canonical),
		DelegatePlan:         &delegatePlan,
		CredentialReferences: []string{"DAEM_TEST_TOKEN"},
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return snapshottest.File(t, record), canonical
}

func applyMCPStdioServer(
	t *testing.T,
	serverID string,
	selected targetpkg.Target,
	command string,
	args []string,
	env map[string]string,
) (desiredmcp.Server, desiredmcp.Binding) {
	t.Helper()
	envReferences := make(map[string]desiredmcp.EnvReference, len(env))
	for name, fromEnv := range env {
		envReferences[name] = desiredtest.MCPEnvReference(t, fromEnv)
	}
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, command),
		args,
		envReferences,
	)
	binding := desiredtest.MCPBinding(
		t,
		selected,
		targetpkg.ScopeProject,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     serverID,
		Bindings: []desiredmcp.Binding{binding},
	})
	return server, binding
}

func applyMCPEnvironment(
	t *testing.T,
	serverID string,
	selected targetpkg.Target,
	command string,
	args []string,
	env map[string]string,
) desired.Environment {
	t.Helper()
	server, _ := applyMCPStdioServer(t, serverID, selected, command, args, env)
	return desiredtest.Environment(t, desired.Spec{
		Targets:    []targetpkg.Target{selected},
		Defaults:   desiredtest.Defaults(t, targetpkg.ScopeProject, skill.InstallModeCopy),
		MCPServers: []desiredmcp.Server{server},
	})
}

func canonicalApplyMCPEntry(t *testing.T, serverID string, command string, args []string) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            args,
		Env:             map[string]string{},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	return canonical
}

func loadApplyStatefile(t *testing.T, path string) durable.Snapshot {
	t.Helper()
	state, err := statefile.Load(t.Context(), path)
	if err != nil {
		t.Fatalf("load statefile: %v", err)
	}
	return state
}

func assertApplyMCPConfigEquivalent(t *testing.T, path string, serverID string, canonical []byte) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read MCP config: %v", err)
	}
	comparison, err := compareApplyMCPPlacementCanonicalEntry(t, aggregate.MCPPlacementClaudeProject, content, serverID, canonical)
	if err != nil {
		t.Fatalf("CompareClaudeProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present equivalent MCP projection", comparison)
	}
}

func assertApplyMCPStateSubject(t *testing.T, snapshot durable.Snapshot, serverID string, canonical []byte) {
	t.Helper()
	assertApplyMCPAggregateStateSubject(
		t,
		snapshot,
		serverID,
		aggregate.MCPPlacementClaudeProject,
		canonical,
	)
}

func assertApplyMCPAggregateStateSubject(
	t *testing.T,
	snapshot durable.Snapshot,
	serverID string,
	placementID aggregate.MCPPlacementID,
	canonical []byte,
) {
	t.Helper()
	for _, stateResource := range snapshot.ManagedAggregates() {
		subject := stateResource.Subject()
		if subject.Key() != serverID {
			continue
		}
		placement, admitted := aggregate.MCPPlacementForSubject(subject)
		if !admitted || placement.ID() != placementID {
			continue
		}
		contribution := stateResource.Contribution()
		if contribution.PlacementID() != string(placementID) ||
			contribution.CanonicalContribution() != string(canonical) {
			t.Fatalf(
				"MCP state resource = %#v, contribution = %#v, want aggregate placement %q canonical %q",
				stateResource,
				contribution,
				placementID,
				canonical,
			)
		}
		return
	}
	t.Fatalf("MCP subject state for %q not found in %#v", serverID, snapshot.ManagedAggregates())
}
