package mcp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	adopt "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestAdmitMCPArgumentCandidatesClassifiesEveryInvalidArgumentForm(t *testing.T) {
	route, err := newImportSource("config.json").route(
		"/mcp/invalid",
		importDocument{revision: "test-revision"},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	for name, argument := range map[string]string{
		"invalid UTF-8":         string([]byte{0xff}),
		"control":               "safe\x00text",
		"bidirectional control": "safe\u202etext",
	} {
		t.Run(name, func(t *testing.T) {
			servers, skipped := admitMCPArgumentCandidates([]adopt.MCPServer{{
				ResourceName: "invalid",
				Target:       target.TargetClaudeCode,
				Scope:        target.ScopeProject,
				SourceRoute:  route,
				Command:      "node",
				Args:         []string{argument},
			}}, nil)
			if len(servers) != 0 || len(skipped) != 1 ||
				skipped[0].LivePath != "config.json#/mcp/invalid" ||
				skipped[0].Reason != skipInvalidArgument {
				t.Fatalf("admission = (%#v, %#v), want one typed skip", servers, skipped)
			}
		})
	}
}

func TestCandidatesSkipInvalidArgumentsAcrossEveryImportRoute(t *testing.T) {
	const canary = "ARGUMENT_LEAK_CANARY"
	tests := []struct {
		name        string
		target      target.Target
		scope       target.Scope
		projectPath string
		globalPath  string
		content     string
		wantPath    string
	}{
		{
			name:        "Claude project",
			target:      target.TargetClaudeCode,
			scope:       target.ScopeProject,
			projectPath: aggregate.ClaudeProjectMCPConfigPath,
			content:     `{"mcpServers":{"invalid":{"type":"stdio","command":"node","args":["\u0000` + canary + `"]},"valid":{"type":"stdio","command":"node","args":["server.js"]}}}`,
			wantPath:    aggregate.ClaudeProjectMCPConfigPath + "#/mcpServers/invalid",
		},
		{
			name:       "Claude global",
			target:     target.TargetClaudeCode,
			scope:      target.ScopeGlobal,
			globalPath: ".claude.json",
			content:    `{"mcpServers":{"invalid":{"type":"stdio","command":"node","args":["\u0000` + canary + `"]},"valid":{"type":"stdio","command":"node","args":["server.js"]}}}`,
			wantPath:   "#/mcpServers/invalid",
		},
		{
			name:        "OpenCode project",
			target:      target.TargetOpenCode,
			scope:       target.ScopeProject,
			projectPath: aggregate.OpenCodeProjectMCPConfigPath,
			content:     `{"mcp":{"invalid":{"type":"local","command":["node","\u0000` + canary + `"]},"valid":{"type":"local","command":["node","server.js"]}}}`,
			wantPath:    aggregate.OpenCodeProjectMCPConfigPath + "#/mcp/invalid",
		},
		{
			name:       "OpenCode global",
			target:     target.TargetOpenCode,
			scope:      target.ScopeGlobal,
			globalPath: filepath.Join(".config", "opencode", "opencode.json"),
			content:    `{"mcp":{"invalid":{"type":"local","command":["node","\u0000` + canary + `"]},"valid":{"type":"local","command":["node","server.js"]}}}`,
			wantPath:   "#/mcp/invalid",
		},
		{
			name:        "Codex project",
			target:      target.TargetCodex,
			scope:       target.ScopeProject,
			projectPath: aggregate.CodexProjectMCPConfigPath,
			content: `[mcp_servers.invalid]
command = "node"
args = ["\u0000` + canary + `"]

[mcp_servers.valid]
command = "node"
args = ["server.js"]
`,
			wantPath: aggregate.CodexProjectMCPConfigPath + "#/mcp_servers/invalid",
		},
		{
			name:       "Codex global",
			target:     target.TargetCodex,
			scope:      target.ScopeGlobal,
			globalPath: filepath.Join(".codex", "config.toml"),
			content: `[mcp_servers.invalid]
command = "node"
args = ["\u0000` + canary + `"]

[mcp_servers.valid]
command = "node"
args = ["server.js"]
`,
			wantPath: "#/mcp_servers/invalid",
		},
		{
			name:       "Antigravity global",
			target:     target.TargetAntigravityCLI,
			scope:      target.ScopeGlobal,
			globalPath: filepath.Join(".gemini", "config", "mcp_config.json"),
			content:    `{"mcpServers":{"invalid":{"command":"node","args":["\u0000` + canary + `"]},"valid":{"command":"node","args":["server.js"]}}}`,
			wantPath:   "#/mcpServers/invalid",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			var livePath string
			if test.scope == target.ScopeProject {
				withWorkingDirectory(t, root)
				livePath = test.projectPath
			} else {
				home := filepath.Join(root, "home")
				t.Setenv("HOME", home)
				livePath = filepath.Join(home, test.globalPath)
			}
			if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(livePath, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}

			servers, authorities, skipped, err := Candidates(t.Context(), test.target, test.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(servers) != 1 || servers[0].ResourceName != "valid" {
				t.Fatalf("servers = %#v, want only valid sibling", servers)
			}
			if len(authorities) != 1 ||
				authorities[0].Target != test.target ||
				authorities[0].Scope != test.scope ||
				authorities[0].PrimaryPath != livePath ||
				authorities[0].PrimaryRevision != servers[0].SourceRoute.PrimaryRevision ||
				authorities[0].MaximumBytes != servers[0].SourceRoute.MaximumBytes ||
				!slices.Equal(authorities[0].RequiredAbsentPaths, servers[0].SourceRoute.RequiredAbsentPaths) {
				t.Fatalf("authorities = %#v, want one exact document authority", authorities)
			}
			wantPath := test.wantPath
			if test.scope == target.ScopeGlobal {
				wantPath = livePath + wantPath
			}
			if len(skipped) != 1 || skipped[0].LivePath != wantPath || skipped[0].Reason != skipInvalidArgument {
				t.Fatalf("skipped = %#v, want invalid argument skip at %q", skipped, wantPath)
			}
			if strings.Contains(skipped[0].Reason, canary) {
				t.Fatalf("skip reason disclosed argument content: %q", skipped[0].Reason)
			}
		})
	}
}
