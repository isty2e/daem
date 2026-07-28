package authoring

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPServerFromAddRequestRejectsUnsupportedFirstSliceValues(t *testing.T) {
	tests := []struct {
		name    string
		request AddMCPServerRequest
		want    string
	}{
		{
			name:    "pi target",
			request: AddMCPServerRequest{Name: "context7", Command: "npx", Targets: []string{"pi"}},
			want:    "supports only --target claude-code, --target antigravity-cli, --target opencode, or --target codex",
		},
		{
			name:    "antigravity missing scope",
			request: AddMCPServerRequest{Name: "context7", Command: "npx", Targets: []string{"antigravity-cli"}},
			want:    "requires --scope global for --target antigravity-cli",
		},
		{
			name:    "antigravity project scope",
			request: AddMCPServerRequest{Name: "context7", Command: "npx", Targets: []string{"antigravity-cli"}, Scope: "project"},
			want:    "supports --scope global for --target antigravity-cli",
		},
		{
			name: "antigravity env alias",
			request: AddMCPServerRequest{
				Name:    "context7",
				Command: "npx",
				Targets: []string{"antigravity-cli"},
				Scope:   "global",
				Env:     []MCPServerEnvAssignment{{Name: "API_TOKEN", FromEnv: "CONTEXT7_API_TOKEN"}},
			},
			want: "supports only same-name environment references",
		},
		{
			name: "opencode env",
			request: AddMCPServerRequest{
				Name:    "context7",
				Command: "npx",
				Targets: []string{"opencode"},
				Env:     []MCPServerEnvAssignment{{Name: "API_TOKEN", FromEnv: "CONTEXT7_API_TOKEN"}},
			},
			want: "does not support --env for --target opencode",
		},
		{
			name: "codex env",
			request: AddMCPServerRequest{
				Name:    "context7",
				Command: "npx",
				Targets: []string{"codex"},
				Env:     []MCPServerEnvAssignment{{Name: "API_TOKEN", FromEnv: "CONTEXT7_API_TOKEN"}},
			},
			want: "does not support --env for --target codex",
		},
		{
			name:    "absolute command",
			request: AddMCPServerRequest{Name: "context7", Command: "/usr/bin/node"},
			want:    "portable command token",
		},
		{
			name:    "shell command",
			request: AddMCPServerRequest{Name: "context7", Command: "node server.js"},
			want:    "portable command token",
		},
		{
			name:    "command whitespace",
			request: AddMCPServerRequest{Name: "context7", Command: " npx"},
			want:    "must not contain leading or trailing whitespace",
		},
		{
			name: "duplicate env",
			request: AddMCPServerRequest{
				Name:    "context7",
				Command: "npx",
				Env: []MCPServerEnvAssignment{
					{Name: "API_TOKEN", FromEnv: "ONE"},
					{Name: "API_TOKEN", FromEnv: "TWO"},
				},
			},
			want: "duplicate --env",
		},
		{
			name: "literal-looking env",
			request: AddMCPServerRequest{
				Name:    "context7",
				Command: "npx",
				Env:     []MCPServerEnvAssignment{{Name: "API_TOKEN", FromEnv: "${CONTEXT7_API_TOKEN}"}},
			},
			want: "env name must contain only ASCII",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := MCPServerFromAddRequest(test.request, declaration.ManifestHeader{Targets: []string{"claude-code"}}, daempaths.ManifestOriginExplicit)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMCPAuthoringDerivesEveryTargetScopeFromCanonicalPlacementCatalog(t *testing.T) {
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		targets, err := addMCPAuthoringTargets([]string{string(placement.Target())})
		if err != nil || len(targets) != 1 || targets[0] != string(placement.Target()) {
			t.Fatalf("addMCPAuthoringTargets(%q) = (%#v, %v)", placement.Target(), targets, err)
		}
		scope, err := addMCPAuthoringScope(string(placement.Target()), string(placement.Scope()))
		if err != nil || scope != string(placement.Scope()) {
			t.Fatalf("addMCPAuthoringScope(%q, %q) = (%q, %v)", placement.Target(), placement.Scope(), scope, err)
		}
		request := AddMCPServerRequest{
			Name:    "context7",
			Command: "npx",
			Targets: targets,
			Scope:   scope,
		}
		if placement.EnvReferenceContract().Supported() {
			sourceName := "HOST_TOKEN"
			if placement.EnvReferenceContract().Mapping() == aggregate.MCPEnvMappingSameName {
				sourceName = "TOKEN"
			}
			request.Env = []MCPServerEnvAssignment{{Name: "TOKEN", FromEnv: sourceName}}
		}
		if err := validateAddMCPAuthoringShape(targets[0], scope, request); err != nil {
			t.Fatalf("validateAddMCPAuthoringShape(%q, %q) returned error: %v", targets[0], scope, err)
		}
	}

	const want = "--target claude-code, --target antigravity-cli, --target opencode, or --target codex"
	if got := mcpAuthoringTargetOptions(); got != want {
		t.Fatalf("mcpAuthoringTargetOptions = %q, want %q", got, want)
	}
}

func TestMCPServerFromAddRequestSupportsCodexProjectRow(t *testing.T) {
	server, err := MCPServerFromAddRequest(AddMCPServerRequest{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		Targets: []string{"codex"},
	}, declaration.ManifestHeader{}, daempaths.ManifestOriginExplicit)
	if err != nil {
		t.Fatalf("MCPServerFromAddRequest returned error: %v", err)
	}
	if server.Name != "context7" ||
		server.Command != declaration.NewMCPAmbientCommand("npx") ||
		server.Transport != "stdio" ||
		server.Scope != "project" ||
		len(server.Targets) != 1 ||
		server.Targets[0] != "codex" {
		t.Fatalf("server = %#v, want Codex project MCP row", server)
	}
	if len(server.Env) != 0 {
		t.Fatalf("server.Env = %#v, want empty", server.Env)
	}
}

func TestMCPServerFromAddRequestDerivesScopeFromWorkspaceOrigin(t *testing.T) {
	tests := []struct {
		name         string
		origin       daempaths.ManifestOrigin
		defaultScope string
		wantScope    string
	}{
		{name: "project manifest does not inherit global authority", origin: daempaths.ManifestOriginExplicit, defaultScope: "global", wantScope: "project"},
		{name: "user default manifest selects global authority", origin: daempaths.ManifestOriginUserDefault, defaultScope: "project", wantScope: "global"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, err := MCPServerFromAddRequest(AddMCPServerRequest{
				Name:    "context7",
				Command: "npx",
				Targets: []string{"codex"},
			}, declaration.ManifestHeader{
				Targets:  []string{"codex"},
				Defaults: declaration.Defaults{Scope: test.defaultScope},
			}, test.origin)
			if err != nil {
				t.Fatalf("MCPServerFromAddRequest returned error: %v", err)
			}
			if server.Scope != test.wantScope {
				t.Fatalf("server.Scope = %q, want %q", server.Scope, test.wantScope)
			}
		})
	}
}

func TestMCPServerFromAddRequestSupportsCodexGlobalRow(t *testing.T) {
	server, err := MCPServerFromAddRequest(AddMCPServerRequest{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		Targets: []string{"codex"},
		Scope:   "global",
	}, declaration.ManifestHeader{}, daempaths.ManifestOriginExplicit)
	if err != nil {
		t.Fatalf("MCPServerFromAddRequest returned error: %v", err)
	}
	if server.Name != "context7" ||
		server.Command != declaration.NewMCPAmbientCommand("npx") ||
		server.Transport != "stdio" ||
		server.Scope != "global" ||
		len(server.Targets) != 1 ||
		server.Targets[0] != "codex" {
		t.Fatalf("server = %#v, want Codex global MCP row", server)
	}
	if len(server.Env) != 0 {
		t.Fatalf("server.Env = %#v, want empty", server.Env)
	}
}

func TestMCPServerFromAddRequestSupportsClaudeGlobalRow(t *testing.T) {
	server, err := MCPServerFromAddRequest(AddMCPServerRequest{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		Targets: []string{"claude-code"},
		Scope:   "global",
	}, declaration.ManifestHeader{}, daempaths.ManifestOriginExplicit)
	if err != nil {
		t.Fatalf("MCPServerFromAddRequest returned error: %v", err)
	}
	if server.Name != "context7" ||
		server.Command != declaration.NewMCPAmbientCommand("npx") ||
		server.Transport != "stdio" ||
		server.Scope != "global" ||
		len(server.Targets) != 1 ||
		server.Targets[0] != "claude-code" {
		t.Fatalf("server = %#v, want Claude global MCP row", server)
	}
	if len(server.Env) != 0 {
		t.Fatalf("server.Env = %#v, want empty", server.Env)
	}
}

func TestMCPServerFromAddRequestSupportsOpenCodeProjectRow(t *testing.T) {
	server, err := MCPServerFromAddRequest(AddMCPServerRequest{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		Targets: []string{"opencode"},
	}, declaration.ManifestHeader{}, daempaths.ManifestOriginExplicit)
	if err != nil {
		t.Fatalf("MCPServerFromAddRequest returned error: %v", err)
	}
	if server.Name != "context7" ||
		server.Command != declaration.NewMCPAmbientCommand("npx") ||
		server.Transport != "stdio" ||
		server.Scope != "project" ||
		len(server.Targets) != 1 ||
		server.Targets[0] != "opencode" {
		t.Fatalf("server = %#v, want OpenCode project MCP row", server)
	}
	if len(server.Env) != 0 {
		t.Fatalf("server.Env = %#v, want empty", server.Env)
	}
}

func TestMCPServerFromAddRequestSupportsOpenCodeGlobalRow(t *testing.T) {
	server, err := MCPServerFromAddRequest(AddMCPServerRequest{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		Targets: []string{"opencode"},
		Scope:   "global",
	}, declaration.ManifestHeader{}, daempaths.ManifestOriginExplicit)
	if err != nil {
		t.Fatalf("MCPServerFromAddRequest returned error: %v", err)
	}
	if server.Name != "context7" ||
		server.Command != declaration.NewMCPAmbientCommand("npx") ||
		server.Transport != "stdio" ||
		server.Scope != "global" ||
		len(server.Targets) != 1 ||
		server.Targets[0] != "opencode" {
		t.Fatalf("server = %#v, want OpenCode global MCP row", server)
	}
	if len(server.Env) != 0 {
		t.Fatalf("server.Env = %#v, want empty", server.Env)
	}
}

func TestMCPServerFromAddRequestSupportsAntigravityGlobalRow(t *testing.T) {
	server, err := MCPServerFromAddRequest(AddMCPServerRequest{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		Targets: []string{"antigravity-cli"},
		Scope:   "global",
		Env:     []MCPServerEnvAssignment{{Name: "CONTEXT7_API_TOKEN", FromEnv: "CONTEXT7_API_TOKEN"}},
	}, declaration.ManifestHeader{}, daempaths.ManifestOriginExplicit)
	if err != nil {
		t.Fatalf("MCPServerFromAddRequest returned error: %v", err)
	}
	if server.Name != "context7" ||
		server.Command != declaration.NewMCPAmbientCommand("npx") ||
		server.Transport != "stdio" ||
		server.Scope != "global" ||
		len(server.Targets) != 1 ||
		server.Targets[0] != "antigravity-cli" {
		t.Fatalf("server = %#v, want Antigravity global MCP row", server)
	}
	if len(server.Env) != 1 ||
		server.Env["CONTEXT7_API_TOKEN"].FromEnv != "CONTEXT7_API_TOKEN" {
		t.Fatalf("server.Env = %#v, want same-name Antigravity environment reference", server.Env)
	}
}
