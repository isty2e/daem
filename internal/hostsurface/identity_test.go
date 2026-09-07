package hostsurface

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPSurfaceKeyAndIDStayDistinctFromPlacementTokens(t *testing.T) {
	t.Parallel()

	key, err := MCPSurfaceKey(target.TargetClaudeCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if key.Kind() != entity.KindMCPServer || key.Variant() != VariantDefault {
		t.Fatalf("key = %+v", key)
	}
	id, err := NewSurfaceID(key)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != "surface/mcp_server/claude-code/project/default" {
		t.Fatalf("id = %q", id)
	}
	if id.String() == "claude-code.project.project-config" {
		t.Fatal("surface ID must not equal the Claude project placement ID")
	}
	roundTrip, err := NewSurfaceID(id.Key())
	if err != nil || !roundTrip.Equal(id) {
		t.Fatalf("round-trip ID = %#v err=%v", roundTrip, err)
	}
}

func TestParseVariantIDRejectsEmptyAndBlank(t *testing.T) {
	t.Parallel()

	if _, err := ParseVariantID(""); err == nil {
		t.Fatal("empty variant accepted")
	}
	if _, err := ParseVariantID(" default"); err == nil {
		t.Fatal("padded variant accepted")
	}
	if _, err := NewSurfaceKey(target.TargetClaudeCode, target.ScopeProject, entity.KindMCPServer, ""); err == nil {
		t.Fatal("empty variant key accepted")
	}
}
