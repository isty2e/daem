package manifest

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
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
	if server.ID().Name() != "repo-tools" || bindings[0].Target() != target.TargetClaudeCode || !ok || stdio.Command().Executable() != "node" {
		t.Fatalf("MCP server = %#v, want normalized Claude Code stdio server", server)
	}
	if len(environment.Skills()) != 0 || len(environment.Hooks()) != 0 || len(environment.Instructions()) != 0 {
		t.Fatalf("environment = %#v, want only MCP", environment)
	}
}

func TestParsePreservesExplicitAbsoluteMCPCommandPath(t *testing.T) {
	absolutePath := filepath.Join(t.TempDir(), "bin with spaces", "codegraph")
	environment, err := Decode([]byte(mcpManifestWithCommand(
		`{ path = ` + strconv.Quote(absolutePath) + ` }`,
	)))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	stdio, ok := environment.MCPServers()[0].Bindings()[0].Transport().Stdio()
	if !ok {
		t.Fatal("decoded MCP transport is not stdio")
	}
	command := stdio.Command()
	if command.Resolution() != desiredmcp.CommandResolutionAbsolutePath ||
		command.Executable() != absolutePath {
		t.Fatalf("command = (%q, %q), want exact absolute path", command.Resolution(), command.Executable())
	}
}

func TestParseRejectsAmbiguousOrInvalidMCPCommandForms(t *testing.T) {
	base := t.TempDir()
	separator := string(filepath.Separator)
	absolutePath := filepath.Join(base, "bin", "codegraph")
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "absolute string stays portable form",
			command: strconv.Quote(absolutePath),
			want:    "portable command token",
		},
		{
			name:    "relative path object",
			command: `{ path = "bin/codegraph" }`,
			want:    "must be absolute",
		},
		{
			name:    "traversal path object",
			command: `{ path = ` + strconv.Quote(base+separator+".."+separator+"codegraph") + ` }`,
			want:    "must be canonical",
		},
		{
			name:    "surrounding whitespace",
			command: `{ path = ` + strconv.Quote(" "+absolutePath) + ` }`,
			want:    "surrounding whitespace",
		},
		{
			name:    "control character",
			command: `{ path = ` + strconv.Quote(filepath.Join(base, "code\ngraph")) + ` }`,
			want:    "control",
		},
		{
			name:    "bidirectional formatting character",
			command: `{ path = ` + strconv.Quote(absolutePath+"\u202e") + ` }`,
			want:    "bidirectional",
		},
		{
			name:    "redundant portable flag",
			command: `{ path = ` + strconv.Quote(absolutePath) + `, portable = false }`,
			want:    "exactly one path key",
		},
		{
			name:    "empty object",
			command: `{}`,
			want:    "exactly one path key",
		},
		{
			name:    "unknown object",
			command: `{ local_parameter = "codegraph" }`,
			want:    "unknown command object",
		},
		{
			name:    "non-string path",
			command: `{ path = 7 }`,
			want:    "must be a string",
		},
		{
			name:    "array command",
			command: `["codegraph"]`,
			want:    "portable token string or an object",
		},
		{
			name:    "boolean command",
			command: `true`,
			want:    "portable token string or an object",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(mcpManifestWithCommand(test.command)))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func mcpManifestWithCommand(command string) string {
	return `version = 1
targets = ["antigravity-cli"]

[[mcp_server]]
name = "codegraph"
targets = ["antigravity-cli"]
scope = "global"
transport = "stdio"
command = ` + command + `
args = ["serve", "--mcp"]
`
}
