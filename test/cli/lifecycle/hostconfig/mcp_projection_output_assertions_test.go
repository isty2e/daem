package cli_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func assertPlanSubjectAction(t *testing.T, payload clijson.Plan, kind string, serverID string) {
	t.Helper()
	for _, action := range payload.Actions {
		if action.Kind != kind || action.Subject == nil {
			continue
		}
		if action.Subject.Kind == string(topology.SubjectProjection) &&
			action.Subject.Namespace == "claude-code.project.mcp-server" &&
			action.Subject.Name == serverID &&
			action.ResourceID == "" &&
			action.Projection != nil &&
			action.Projection.ConfigPath == aggregate.ClaudeProjectMCPConfigPath &&
			action.Projection.ContentPath == mcpcodec.ClaudeProjectMCPContentPath(serverID) {
			return
		}
	}
	t.Fatalf("actions = %#v, want %s subject action for %q", payload.Actions, kind, serverID)
}

func assertPlanSubjectActionReason(t *testing.T, payload clijson.Plan, kind string, reason string, serverID string) {
	t.Helper()
	for _, action := range payload.Actions {
		if action.Kind != kind || action.Reason != reason || action.Subject == nil {
			continue
		}
		if action.Subject.Kind == string(topology.SubjectProjection) &&
			action.Subject.Namespace == "claude-code.project.mcp-server" &&
			action.Subject.Name == serverID &&
			action.Projection != nil &&
			action.Projection.ConfigPath == aggregate.ClaudeProjectMCPConfigPath &&
			action.Projection.ContentPath == mcpcodec.ClaudeProjectMCPContentPath(serverID) {
			return
		}
	}
	t.Fatalf("actions = %#v, want %s/%s subject action for %q", payload.Actions, kind, reason, serverID)
}

func assertApplyResultSubjectActionReason(t *testing.T, payload clijson.ApplyResult, kind string, reason string, serverID string) {
	t.Helper()
	for _, action := range payload.Actions {
		if action.Kind != kind || action.Reason != reason || action.Subject == nil {
			continue
		}
		if action.Subject.Kind == string(topology.SubjectProjection) &&
			action.Subject.Namespace == "claude-code.project.mcp-server" &&
			action.Subject.Name == serverID &&
			action.Projection != nil &&
			action.Projection.ConfigPath == aggregate.ClaudeProjectMCPConfigPath &&
			action.Projection.ContentPath == mcpcodec.ClaudeProjectMCPContentPath(serverID) {
			return
		}
	}
	t.Fatalf("actions = %#v, want %s/%s subject action for %q", payload.Actions, kind, reason, serverID)
}

func assertApplyResultSubjectAction(t *testing.T, payload clijson.ApplyResult, kind string, serverID string) {
	t.Helper()
	for _, action := range payload.Actions {
		if action.Kind != kind || action.Subject == nil {
			continue
		}
		if action.Subject.Kind == string(topology.SubjectProjection) &&
			action.Subject.Namespace == "claude-code.project.mcp-server" &&
			action.Subject.Name == serverID &&
			action.Projection != nil &&
			action.Projection.ConfigPath == aggregate.ClaudeProjectMCPConfigPath &&
			action.Projection.ContentPath == mcpcodec.ClaudeProjectMCPContentPath(serverID) {
			return
		}
	}
	t.Fatalf("actions = %#v, want %s subject action for %q", payload.Actions, kind, serverID)
}

func assertMCPJSONDimension(t *testing.T, payload clijson.Plan, dimension string, state string, reason string) {
	t.Helper()
	if len(payload.MCPStatuses) != 1 {
		t.Fatalf("mcp_statuses = %#v, want one", payload.MCPStatuses)
	}
	dimensions := payload.MCPStatuses[0].Dimensions()
	for _, got := range dimensions {
		if got.Dimension != dimension {
			continue
		}
		if got.State != state || got.Reason != reason {
			t.Fatalf("%s dimension = %#v, want state=%q reason=%q", dimension, got, state, reason)
		}
		return
	}
	t.Fatalf("dimensions = %#v, want %s", dimensions, dimension)
}

func assertMCPJSONDimensionInGroup(
	t *testing.T,
	payload clijson.Plan,
	group string,
	dimension string,
	state string,
	reason string,
) {
	t.Helper()
	if len(payload.MCPStatuses) != 1 {
		t.Fatalf("mcp_statuses = %#v, want one", payload.MCPStatuses)
	}
	var dimensions []clijson.MCPStatusDimension
	switch group {
	case "projection":
		dimensions = payload.MCPStatuses[0].Projection
	case "host":
		dimensions = payload.MCPStatuses[0].Host
	case "delegate":
		dimensions = payload.MCPStatuses[0].Delegate
	case "runtime":
		dimensions = payload.MCPStatuses[0].Runtime
	case "residue":
		dimensions = payload.MCPStatuses[0].Residue
	case "other":
		dimensions = payload.MCPStatuses[0].Other
	default:
		t.Fatalf("unknown MCP status group %q", group)
	}
	for _, got := range dimensions {
		if got.Dimension != dimension {
			continue
		}
		if got.State != state || got.Reason != reason {
			t.Fatalf("%s dimension = %#v, want state=%q reason=%q", dimension, got, state, reason)
		}
		return
	}
	t.Fatalf("%s dimensions = %#v, want %s", group, dimensions, dimension)
}

func assertApplyResultMCPJSONDimension(t *testing.T, payload clijson.ApplyResult, dimension string, state string, reason string) {
	t.Helper()
	if len(payload.MCPStatuses) != 1 {
		t.Fatalf("mcp_statuses = %#v, want one", payload.MCPStatuses)
	}
	dimensions := payload.MCPStatuses[0].Dimensions()
	for _, got := range dimensions {
		if got.Dimension != dimension {
			continue
		}
		if got.State != state || got.Reason != reason {
			t.Fatalf("%s dimension = %#v, want state=%q reason=%q", dimension, got, state, reason)
		}
		return
	}
	t.Fatalf("dimensions = %#v, want %s", dimensions, dimension)
}

func assertNoPublicMCPOutputLeaks(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{
		`"resource_id":"/"`,
		`resource="/"`,
		`resource="mcp/`,
		`"state":"unknown"`,
		`: unknown`,
		"server works",
		"server is running",
		"tools are available",
		"canonical_projection",
		"literal-secret-canary",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output contains forbidden public MCP text %q:\n%s", forbidden, output)
		}
	}
}
