package clipresent

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintMCPStatusesKeepsDelegateAttemptSeparateFromProjection(t *testing.T) {
	status := MCPStatus{
		Subject: MCPSubject{
			Kind:      "projection",
			Namespace: "claude-project-mcp",
			Name:      "context7",
		},
		Target:                 "claude-code",
		Scope:                  "project",
		ConfigPath:             ".mcp.json",
		ContentPath:            "/mcpServers/context7",
		AdapterContractVersion: "claude-project-mcp-stdio-v1",
		Projection: []MCPStatusDimension{
			{Dimension: "project_projection", State: "projected"},
		},
		Delegate: []MCPStatusDimension{
			{Dimension: "delegate_last_attempt", State: "stale", Reason: "LAST_DELEGATE_ATTEMPT_STALE"},
		},
		Runtime: []MCPStatusDimension{
			{Dimension: "runtime_launcher", State: "not_probed", Reason: "RUNTIME_NOT_PROBED"},
		},
	}

	var stdout bytes.Buffer
	PrintMCPStatusesWithOptions(&stdout, []MCPStatus{status}, HumanOptions{Verbose: true})
	rendered := stdout.String()

	for _, want := range []string{
		"projection:",
		"project_projection: projected",
		"delegate:",
		"delegate_last_attempt: stale reason=LAST_DELEGATE_ATTEMPT_STALE",
		"runtime:",
		"runtime_launcher: not_probed reason=RUNTIME_NOT_PROBED",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("mcp status output = %q, want %q", rendered, want)
		}
	}
	for _, forbidden := range []string{"ready", "current", "converged", "installed"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("mcp status output = %q, want no %q", rendered, forbidden)
		}
	}
}
