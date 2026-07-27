package snapshottest

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

// MCPProjectionInput carries already-canonical MCP projection fixture facts.
type MCPProjectionInput struct {
	PlacementID          aggregate.MCPPlacementID
	ServerID             string
	LauncherCommand      string
	LauncherArgs         []string
	CanonicalProjection  string
	CredentialReferences []string
	DelegatePlan         *delegate.DelegatePlan
}

// MCPProjection constructs one canonically refined MCP projection fixture.
func MCPProjection(t testing.TB, input MCPProjectionInput) lock.LockedSubjectContract {
	t.Helper()
	operations, ok := mcpcodec.ImplementedMCPPlacementOperationsForID(input.PlacementID)
	if !ok {
		t.Fatalf("MCP placement %q is unavailable", input.PlacementID)
	}
	placement := operations.Placement()
	entityID, err := entity.New(entity.KindMCPServer, input.ServerID)
	if err != nil {
		t.Fatalf("MCP entity: %v", err)
	}
	projection, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), entityID.Name())
	if err != nil {
		t.Fatalf("MCP projection subject: %v", err)
	}
	launcher, err := topologymcp.ExecutableSubject(input.LauncherCommand)
	if err != nil {
		t.Fatalf("MCP launcher subject: %v", err)
	}
	subjects := []topology.SubjectID{projection, launcher}
	edges := []topology.Edge{topology.NewEdge(topology.EdgeLaunchesVia, projection, launcher)}
	for _, reference := range input.CredentialReferences {
		credential, err := topologymcp.EnvironmentReferenceSubject(reference)
		if err != nil {
			t.Fatalf("MCP credential subject: %v", err)
		}
		subjects = append(subjects, credential)
		edges = append(edges, topology.NewEdge(topology.EdgeDependsOn, projection, credential))
	}
	graph, err := topology.NewGraph(subjects, edges)
	if err != nil {
		t.Fatalf("MCP projection graph: %v", err)
	}
	contract, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:                graph,
		EntityID:             entityID,
		PlacementID:          input.PlacementID,
		ServerID:             input.ServerID,
		RequestedOnAbsent:    desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:      input.LauncherCommand,
		LauncherArgs:         append([]string(nil), input.LauncherArgs...),
		CanonicalProjection:  input.CanonicalProjection,
		DelegatePlan:         input.DelegatePlan,
		CredentialReferences: append([]string(nil), input.CredentialReferences...),
	})
	if err != nil {
		t.Fatalf("lock.NewMCPProjectionSubjectContract: %v", err)
	}
	return contract
}
