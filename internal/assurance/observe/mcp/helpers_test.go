package mcp

import (
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func claudeMCPRecord(t *testing.T) lock.LockedSubjectContract {
	t.Helper()
	server, binding := mcpRecordServer(t, target.TargetClaudeCode, target.ScopeProject, true)
	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
	if err != nil {
		t.Fatalf("MCPServer returned error: %v", err)
	}
	delegatePlan, err := mcpdelegate.MCPBindingDelegatePlan(server, binding)
	if err != nil {
		t.Fatalf("MCPServerDelegatePlan returned error: %v", err)
	}
	projection := mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		Env:             map[string]string{"API_TOKEN": "${CONTEXT7_API_TOKEN}"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	}
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	delegateIdentity := lock.DelegatePlanIdentityFromPlan(delegatePlan)
	return mcpProjectionContract(
		t,
		graph,
		server,
		aggregate.MCPPlacementClaudeProject,
		string(canonical),
		&delegateIdentity,
		[]string{"CONTEXT7_API_TOKEN"},
	)
}

func antigravityMCPRecord(t *testing.T) lock.LockedSubjectContract {
	t.Helper()
	server, _ := mcpRecordServer(t, target.TargetAntigravityCLI, target.ScopeGlobal, false)
	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
	if err != nil {
		t.Fatalf("MCPServer returned error: %v", err)
	}
	projection := mcpcodec.AntigravityGlobalMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		AdapterContract: aggregate.AntigravityGlobalMCPCommandAdapterV1,
	}
	canonical, err := mcpcodec.CanonicalAntigravityGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalAntigravityGlobalMCPServerEntry returned error: %v", err)
	}
	return mcpProjectionContract(t, graph, server, aggregate.MCPPlacementAntigravityGlobal, string(canonical), nil, nil)
}

func openCodeMCPRecord(t *testing.T) lock.LockedSubjectContract {
	t.Helper()
	server, _ := mcpRecordServer(t, target.TargetOpenCode, target.ScopeProject, false)
	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
	if err != nil {
		t.Fatalf("MCPServer returned error: %v", err)
	}
	projection := mcpcodec.OpenCodeProjectMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		AdapterContract: aggregate.OpenCodeProjectMCPLocalCommandV1,
	}
	canonical, err := mcpcodec.CanonicalOpenCodeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}
	return mcpProjectionContract(t, graph, server, aggregate.MCPPlacementOpenCodeProject, string(canonical), nil, nil)
}

func mcpProjectionContract(
	t *testing.T,
	graph topology.Graph,
	server desiredmcp.Server,
	placementID aggregate.MCPPlacementID,
	canonical string,
	delegateIdentity *lock.DelegatePlanIdentity,
	credentialReferences []string,
) lock.LockedSubjectContract {
	t.Helper()
	contract, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:                graph,
		EntityID:             server.ID(),
		PlacementID:          placementID,
		ServerID:             server.ID().Name(),
		RequestedOnAbsent:    desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:      "npx",
		LauncherArgs:         []string{"-y", "@upstash/context7-mcp"},
		CanonicalProjection:  canonical,
		DelegatePlanIdentity: delegateIdentity,
		CredentialReferences: credentialReferences,
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return contract
}

func mcpRecordServer(
	t *testing.T,
	selected target.Target,
	scope target.Scope,
	withEnv bool,
) (desiredmcp.Server, desiredmcp.Binding) {
	t.Helper()
	var env map[string]desiredmcp.EnvReference
	if withEnv {
		env = map[string]desiredmcp.EnvReference{
			"API_TOKEN": desiredtest.MCPEnvReference(t, "CONTEXT7_API_TOKEN"),
		}
	}
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		[]string{"-y", "@upstash/context7-mcp"},
		env,
	)
	binding := desiredtest.MCPBinding(t, selected, scope, transport, desiredmcp.OnAbsentRemoveBinding)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     "context7",
		Bindings: []desiredmcp.Binding{binding},
	})
	return server, binding
}
