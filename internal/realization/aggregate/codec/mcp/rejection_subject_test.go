package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPProjectionRejectionUsesExactOrCanonicalParentSubject(t *testing.T) {
	tests := []struct {
		name      string
		placement aggregate.MCPPlacementID
		prefix    string
	}{
		{name: "Claude project", placement: aggregate.MCPPlacementClaudeProject, prefix: "/mcpServers"},
		{name: "Claude global", placement: aggregate.MCPPlacementClaudeGlobal, prefix: "/mcpServers"},
		{name: "OpenCode project", placement: aggregate.MCPPlacementOpenCodeProject, prefix: "/mcp"},
		{name: "OpenCode global", placement: aggregate.MCPPlacementOpenCodeGlobal, prefix: "/mcp"},
		{name: "Codex project", placement: aggregate.MCPPlacementCodexProject, prefix: "/mcp_servers"},
		{name: "Codex global", placement: aggregate.MCPPlacementCodexGlobal, prefix: "/mcp_servers"},
		{name: "Antigravity global", placement: aggregate.MCPPlacementAntigravityGlobal, prefix: "/mcpServers"},
	}
	rejectionErr := newMCPProjectionError(
		MCPProjectionReasonUnsupportedTransport,
		"",
		"unsupported transport",
	)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exact := mcpProjectionRejection(test.placement, "context7", rejectionErr)
			if got := string(exact.ContentPath()); got != test.prefix+"/context7" {
				t.Fatalf("exact rejection subject = %q", got)
			}
			for name, serverID := range map[string]string{
				"slash":     "token=SUBJECT_LEAK_CANARY/path",
				"control":   "token\x00SUBJECT_LEAK_CANARY",
				"bidi":      "token\u202eSUBJECT_LEAK_CANARY",
				"oversized": strings.Repeat("a", maximumMCPRejectionServerIDBytes+1),
			} {
				t.Run(name, func(t *testing.T) {
					rejection := mcpProjectionRejection(test.placement, serverID, rejectionErr)
					if got := string(rejection.ContentPath()); got != test.prefix || strings.Contains(got, "SUBJECT_LEAK_CANARY") {
						t.Fatalf("safe rejection subject = %q, want %q", got, test.prefix)
					}
				})
			}
		})
	}
}

func TestBulkMCPExtractionUsesParentSubjectForInvalidJSONAndCodexIdentifiers(t *testing.T) {
	jsonProjections, jsonRejections, err := collectClaudeProjectMCPServerProjections(
		t.Context(),
		[]byte(`{"mcpServers":{"token=JSON_SUBJECT_LEAK_CANARY":{"type":"http","command":"node"},"valid":{"type":"stdio","command":"node"}}}`),
	)
	if err != nil || len(jsonProjections) != 1 || jsonProjections[0].ServerID != "valid" {
		t.Fatalf("JSON extraction = %#v, %#v, %v", jsonProjections, jsonRejections, err)
	}
	assertProjectionRejection(t, jsonRejections, "/mcpServers", MCPProjectionReasonProjectionEquivalenceUndefined)

	codexProjections, codexRejections, err := collectCodexProjectMCPServerProjections(
		t.Context(),
		[]byte(`[mcp_servers."token=CODEX_SUBJECT_LEAK_CANARY"]
command = "node"

[mcp_servers.valid]
command = "node"
`),
	)
	if err != nil || len(codexProjections) != 1 || codexProjections[0].ServerID != "valid" {
		t.Fatalf("Codex extraction = %#v, %#v, %v", codexProjections, codexRejections, err)
	}
	assertProjectionRejection(t, codexRejections, "/mcp_servers", MCPProjectionReasonProjectionEquivalenceUndefined)
}

func TestCodexExactServerIDLimitRetainsExactRejectionSubject(t *testing.T) {
	serverID := strings.Repeat("a", maximumCodexMCPServerIDBytes)
	rejection := mcpProjectionRejection(
		aggregate.MCPPlacementCodexProject,
		serverID,
		newMCPProjectionError(
			MCPProjectionReasonUnsupportedManagedField,
			"",
			"unsupported field",
		),
	)
	want := "/mcp_servers/" + serverID
	if got := string(rejection.ContentPath()); got != want {
		t.Fatalf("Codex exact-limit rejection subject bytes = %d, want exact path bytes %d", len(got), len(want))
	}
}

func TestOversizedValidCanonicalPathRemainsDistinctFromRejectionLocation(t *testing.T) {
	serverID := strings.Repeat("a", maximumMCPRejectionServerIDBytes+1)
	wantCanonical := "/mcpServers/" + serverID
	if got := ClaudeProjectMCPContentPath(serverID); got != wantCanonical {
		t.Fatalf("canonical path bytes = %d, want %d", len(got), len(wantCanonical))
	}
	rejection := mcpProjectionRejection(
		aggregate.MCPPlacementClaudeProject,
		serverID,
		newMCPProjectionError(MCPProjectionReasonUnsupportedTransport, "", "unsupported field"),
	)
	if got := string(rejection.ContentPath()); got != "/mcpServers" {
		t.Fatalf("rejection path = %q, want canonical parent", got)
	}
}
