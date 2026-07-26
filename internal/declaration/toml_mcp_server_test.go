package declaration

import (
	"strings"
	"testing"
)

func TestDecodeMCPServer(t *testing.T) {
	manifest, err := DecodeManifest([]byte(`
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "repo-tools"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "node"
args = ["scripts/mcp-server.js", "--stdio"]
env = { API_TOKEN = { from_env = "REPO_TOOLS_API_TOKEN" } }
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(manifest.MCPServers) != 1 {
		t.Fatalf("len(MCPServers) = %d, want 1", len(manifest.MCPServers))
	}
	server := manifest.MCPServers[0]
	if server.Name != "repo-tools" {
		t.Fatalf("Name = %q, want repo-tools", server.Name)
	}
	if len(server.Args) != 2 || server.Args[0] != "scripts/mcp-server.js" || server.Args[1] != "--stdio" {
		t.Fatalf("Args = %#v, want script argv", server.Args)
	}
	if server.Env["API_TOKEN"].FromEnv != "REPO_TOOLS_API_TOKEN" {
		t.Fatalf("Env[API_TOKEN].FromEnv = %q", server.Env["API_TOKEN"].FromEnv)
	}
}

func TestDecodeRejectsUnsupportedMCPServerSyntax(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "unsupported field",
			manifest: `
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "repo-tools"
transport = "stdio"
command = "node"
url = "https://example.com/mcp"
`,
			want: "unknown manifest key",
		},
		{
			name: "literal env",
			manifest: `
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "repo-tools"
transport = "stdio"
command = "node"
env = { API_TOKEN = "secret-value" }
`,
			want: "env reference must be an inline table with from_env",
		},
		{
			name: "future command local parameter object",
			manifest: `
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "repo-tools"
transport = "stdio"
command = { local_parameter = "repo_tools_runner" }
args = ["--stdio"]
`,
			want: "command",
		},
		{
			name: "future local parameter table",
			manifest: `
version = 1
targets = ["claude-code"]

[[local_parameter]]
name = "repo_tools_runner"
kind = "project_executable"
path = "tools/repo-tools"
executable = true
`,
			want: "unknown manifest key",
		},
		{
			name: "future package runner table",
			manifest: `
version = 1
targets = ["claude-code"]

[[package_runner]]
name = "repo_tools_runner"
family = "npm"
runner = "npx"
package = "@acme/repo-tools"
version = "1.2.3"
`,
			want: "unknown manifest key",
		},
		{
			name: "future executable artifact table",
			manifest: `
version = 1
targets = ["claude-code"]

[[executable_artifact]]
name = "repo_tools_runner"
source = { git = "https://example.invalid/repo-tools.git", ref = "0123456789abcdef" }
path = "dist/repo-tools"
digest = "sha256:abc123"
`,
			want: "unknown manifest key",
		},
		{
			name: "extra env reference key",
			manifest: `
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "repo-tools"
transport = "stdio"
command = "node"
env = { API_TOKEN = { from_env = "REPO_TOOLS_API_TOKEN", literal = "secret-value" } }
`,
			want: "unknown env reference key",
		},
		{
			name: "quoted env reference key with whitespace",
			manifest: `
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "repo-tools"
transport = "stdio"
command = "node"
env = { API_TOKEN = { " from_env " = "REPO_TOOLS_API_TOKEN" } }
`,
			want: "unknown env reference key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeManifest([]byte(test.manifest))
			if err == nil {
				t.Fatal("Decode returned nil error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want %q", err, test.want)
			}
		})
	}
}
