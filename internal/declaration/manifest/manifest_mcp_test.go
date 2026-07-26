package manifest

import (
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestParseAcceptsMCPServerWithoutLoweringItAsResource(t *testing.T) {
	environment, err := Decode([]byte(`
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "repo-tools"
transport = "stdio"
command = "node"
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	servers := environment.MCPServers()
	if len(servers) != 1 {
		t.Fatalf("len(MCPServers) = %d, want 1", len(servers))
	}
	server := servers[0]
	bindings := server.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("bindings = %#v, want one binding", bindings)
	}
	stdio, ok := bindings[0].Transport().Stdio()
	if server.ID().Name() != "repo-tools" || bindings[0].Target() != target.TargetClaudeCode || !ok || stdio.Command().Name() != "node" {
		t.Fatalf("MCP server = %#v, want normalized Claude Code stdio server", server)
	}
	if len(environment.Skills()) != 0 || len(environment.Hooks()) != 0 || len(environment.Instructions()) != 0 {
		t.Fatalf("environment = %#v, want only MCP", environment)
	}
}
