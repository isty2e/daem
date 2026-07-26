package aggregate_test

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestMCPPlacementForSubjectConsumesTopologyProjectionIdentity(t *testing.T) {
	id, err := entity.New(entity.KindMCPServer, "context7")
	if err != nil {
		t.Fatal(err)
	}
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		subject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), id.Name())
		if err != nil {
			t.Fatalf("ProjectionSubject(%q, %q) returned error: %v", placement.Target(), placement.Scope(), err)
		}
		got, ok := aggregate.MCPPlacementForSubject(subject)
		if !ok || got.ID() != placement.ID() {
			t.Fatalf("MCPPlacementForSubject(%s) = (%q, %v), want (%q, true)", subject, got.ID(), ok, placement.ID())
		}
	}
}

func TestMCPPlacementForSubjectRejectsForeignSubjects(t *testing.T) {
	for _, subject := range []topology.SubjectID{
		mustTopologySubject(t, topology.SubjectHostRelation, "claude-code.project.mcp-server", "context7"),
		mustTopologySubject(t, topology.SubjectProjection, "unknown.mcp-server", "context7"),
	} {
		if placement, ok := aggregate.MCPPlacementForSubject(subject); ok {
			t.Fatalf("MCPPlacementForSubject(%s) = %q, true", subject, placement.ID())
		}
	}
}

func mustTopologySubject(t *testing.T, kind topology.SubjectKind, namespace string, key string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(kind, namespace, key)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	return subject
}
