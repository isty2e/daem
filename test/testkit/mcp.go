package testkit

import (
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	mcptest "github.com/isty2e/daem/test/testkit/mcp"
)

func AssertSingleMCPStdioBinding(
	t testing.TB,
	server desiredmcp.Server,
	wantName string,
	wantTarget target.Target,
	wantScope target.Scope,
	wantCommand string,
	wantArgs []string,
) desiredmcp.Stdio {
	t.Helper()
	if server.ID().Name() != wantName {
		t.Fatalf("server ID = %q, want %q", server.ID().Name(), wantName)
	}
	bindings := server.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("server bindings = %#v, want one", bindings)
	}
	binding := bindings[0]
	if binding.Target() != wantTarget || binding.Scope() != wantScope {
		t.Fatalf("server = %#v, want target=%s scope=%s standalone", server, wantTarget, wantScope)
	}
	stdio, ok := binding.Transport().Stdio()
	if !ok || stdio.Command().Executable() != wantCommand {
		t.Fatalf("server transport = %#v, want ambient command %s stdio", binding.Transport(), wantCommand)
	}
	args := stdio.Args()
	if len(args) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
	for index := range wantArgs {
		if args[index] != wantArgs[index] {
			t.Fatalf("args = %#v, want %#v", args, wantArgs)
		}
	}
	return stdio
}

func AssertClaudeGlobalMCPConfigEquivalent(
	t testing.TB,
	hostConfigPath string,
	serverID string,
	command string,
	args []string,
) {
	t.Helper()
	content := ReadFile(t, hostConfigPath)
	canonical, err := mcpcodec.CanonicalClaudeGlobalMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            append([]string(nil), args...),
		AdapterContract: aggregate.ClaudeGlobalMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeGlobalMCPServerEntry returned error: %v", err)
	}
	operations, ok := mcptest.OperationsForPlacementID(aggregate.MCPPlacementClaudeGlobal)
	if !ok {
		t.Fatal("Claude global MCP placement operations missing")
	}
	comparison, err := operations.CompareCanonicalEntry(content, serverID, canonical)
	if err != nil {
		t.Fatalf("CompareClaudeGlobalMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present equivalent Claude global projection", comparison)
	}
}

func AssertClaudeGlobalMCPConfigMissing(t testing.TB, hostConfigPath string, serverID string) {
	t.Helper()
	content := ReadFile(t, hostConfigPath)
	if _, present, err := mcpcodec.ExtractClaudeGlobalMCPServerProjection(content, serverID); err != nil {
		t.Fatalf("ExtractClaudeGlobalMCPServerProjection returned error: %v", err)
	} else if present {
		t.Fatalf("Claude global MCP server %q is still present in %s", serverID, content)
	}
}
