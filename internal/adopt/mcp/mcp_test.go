package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
)

func TestCandidatesImportsClaudeProjectMCPAndReportsRejectedRows(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile(filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath), []byte(`{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y"]},
    "remote": {"type": "http", "command": "npx"}
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, authorities, skipped, err := Candidates(t.Context(), target.TargetClaudeCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].ResourceName != "context7" || servers[0].LivePath() != ".mcp.json#/mcpServers/context7" {
		t.Fatalf("servers = %#v, want context7 project live path", servers)
	}
	if len(skipped) != 1 || skipped[0].LivePath != ".mcp.json#/mcpServers/remote" || skipped[0].Reason != "unsupported_mcp_transport" {
		t.Fatalf("skipped = %#v, want remote unsupported transport", skipped)
	}
	if len(authorities) != 1 || authorities[0].PrimaryPath != aggregate.ClaudeProjectMCPConfigPath {
		t.Fatalf("authorities = %#v, want one document authority for admitted and rejected rows", authorities)
	}
}

func TestCandidatesImportsClaudeGlobalMCPAndReportsRejectedRows(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	livePath := filepath.Join(homeDir, ".claude.json")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, []byte(`{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y", "@upstash/context7-mcp"], "env": {"API_TOKEN": "${CONTEXT7_API_TOKEN}"}},
    "literal": {"type": "stdio", "command": "npx", "env": {"API_TOKEN": "SECRET_CANARY"}},
    "remote": {"type": "http", "url": "https://example.invalid/mcp"}
  },
  "projects": {
    "/repo": {
      "mcpServers": {
        "projectOnly": {"type": "stdio", "command": "node"}
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetClaudeCode, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 ||
		servers[0].ResourceName != "context7" ||
		servers[0].Target != target.TargetClaudeCode ||
		servers[0].Scope != target.ScopeGlobal ||
		servers[0].LivePath() != livePath+"#/mcpServers/context7" ||
		servers[0].Command != "npx" ||
		len(servers[0].Args) != 2 ||
		len(servers[0].Env) != 1 ||
		servers[0].Env["API_TOKEN"] != "CONTEXT7_API_TOKEN" {
		t.Fatalf("servers = %#v, want context7 Claude global live path", servers)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped = %#v, want env and remote skips", skipped)
	}
	if skipped[0].LivePath != livePath+"#/mcpServers/literal" || skipped[0].Reason != "secret_literal_forbidden" {
		t.Fatalf("skipped[0] = %#v, want literal env rejection without secret leak", skipped[0])
	}
	if skipped[1].LivePath != livePath+"#/mcpServers/remote" || skipped[1].Reason != "unsupported_mcp_transport" {
		t.Fatalf("skipped[1] = %#v, want remote unsupported transport", skipped[1])
	}
}

func TestCandidatesImportsCodexProjectMCPAndReportsRejectedRows(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.MkdirAll(filepath.Join(tempDir, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, aggregate.CodexProjectMCPConfigPath), []byte(`
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[mcp_servers.remote]
command = "npx"
env = { API_TOKEN = "SECRET_CANARY" }
`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 ||
		servers[0].ResourceName != "context7" ||
		servers[0].Target != target.TargetCodex ||
		servers[0].Scope != target.ScopeProject ||
		servers[0].LivePath() != ".codex/config.toml#/mcp_servers/context7" ||
		servers[0].Command != "npx" ||
		len(servers[0].Args) != 2 ||
		len(servers[0].Env) != 0 {
		t.Fatalf("servers = %#v, want context7 Codex project live path", servers)
	}
	if len(skipped) != 1 ||
		skipped[0].LivePath != ".codex/config.toml#/mcp_servers/remote" ||
		skipped[0].Reason != "unsupported_mcp_managed_field" {
		t.Fatalf("skipped = %#v, want remote unsupported managed field", skipped)
	}
}

func TestCandidatesRejectsUnsupportedSurfacesExplicitly(t *testing.T) {
	cases := []struct {
		name   string
		target target.Target
		scope  target.Scope
		want   string
	}{
		{name: "antigravity project", target: target.TargetAntigravityCLI, scope: target.ScopeProject, want: "antigravity-cli:project:mcp_server"},
		{name: "pi project", target: target.TargetPi, scope: target.ScopeProject, want: "pi:project:mcp_server"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			servers, _, skipped, err := Candidates(t.Context(), tc.target, tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(servers) != 0 {
				t.Fatalf("servers = %#v, want none", servers)
			}
			if len(skipped) != 1 || skipped[0].LivePath != tc.want || skipped[0].Reason != "unsupported_mcp_server_surface" {
				t.Fatalf("skipped = %#v, want unsupported surface %q", skipped, tc.want)
			}
		})
	}
}

func TestCandidatesImportsCodexGlobalMCPAndReportsRejectedRows(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	livePath := filepath.Join(homeDir, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, []byte(`
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
env_vars = [{ name = "CONTEXT7_TOKEN", source = "local" }]

[mcp_servers.remote]
command = "npx"
env = { API_TOKEN = "SECRET_CANARY" }
`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetCodex, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 ||
		servers[0].ResourceName != "context7" ||
		servers[0].Target != target.TargetCodex ||
		servers[0].Scope != target.ScopeGlobal ||
		servers[0].LivePath() != livePath+"#/mcp_servers/context7" ||
		servers[0].Command != "npx" ||
		len(servers[0].Args) != 2 ||
		len(servers[0].Env) != 1 ||
		servers[0].Env["CONTEXT7_TOKEN"] != "CONTEXT7_TOKEN" {
		t.Fatalf("servers = %#v, want context7 Codex global live path", servers)
	}
	if len(skipped) != 1 ||
		skipped[0].LivePath != livePath+"#/mcp_servers/remote" ||
		skipped[0].Reason != "secret_literal_forbidden" {
		t.Fatalf("skipped = %#v, want literal environment rejection", skipped)
	}
}

func TestCandidatesImportsOpenCodeGlobalMCPAndReportsRejectedRows(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	livePath := filepath.Join(homeDir, ".config", "opencode", "opencode.json")
	conflictingPath := filepath.Join(homeDir, ".config", "opencode", "opencode.jsonc")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, []byte(`{
  "mcp": {
    "context7": {"type": "local", "command": ["npx", "-y", "@upstash/context7-mcp"]},
    "remote": {"type": "remote", "command": ["npx"]},
    "withAlias": {"type": "local", "command": ["npx"], "environment": {"CHILD_TOKEN": "{env:SOURCE_TOKEN}"}},
    "literalEnv": {"type": "local", "command": ["npx"], "environment": {"TOKEN": "SECRET_CANARY"}}
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetOpenCode, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 ||
		servers[0].ResourceName != "context7" ||
		servers[0].Target != target.TargetOpenCode ||
		servers[0].Scope != target.ScopeGlobal ||
		servers[0].LivePath() != livePath+"#/mcp/context7" ||
		servers[0].Command != "npx" ||
		len(servers[0].Args) != 2 ||
		len(servers[0].Env) != 0 ||
		len(servers[0].SourceRoute.RequiredAbsentPaths) != 1 ||
		servers[0].SourceRoute.RequiredAbsentPaths[0] != conflictingPath ||
		servers[1].ResourceName != "withAlias" ||
		servers[1].Env["CHILD_TOKEN"] != "SOURCE_TOKEN" {
		t.Fatalf("servers = %#v, want context7 OpenCode global live path", servers)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped = %#v, want remote and env skips", skipped)
	}
	if skipped[0].LivePath != livePath+"#/mcp/literalEnv" || skipped[0].Reason != "secret_literal_forbidden" {
		t.Fatalf("skipped[0] = %#v, want non-reference environment rejection", skipped[0])
	}
	if skipped[1].LivePath != livePath+"#/mcp/remote" || skipped[1].Reason != "unsupported_mcp_transport" {
		t.Fatalf("skipped[1] = %#v, want remote unsupported transport", skipped[1])
	}
}

func TestCandidatesSkipsOpenCodeMCPWhenAlternateConfigExists(t *testing.T) {
	tests := []struct {
		name      string
		scope     target.Scope
		configure func(*testing.T) (string, string)
	}{
		{
			name:  "project",
			scope: target.ScopeProject,
			configure: func(t *testing.T) (string, string) {
				t.Helper()
				root := t.TempDir()
				withWorkingDirectory(t, root)
				return aggregate.OpenCodeProjectMCPConfigPath, "opencode.jsonc"
			},
		},
		{
			name:  "global",
			scope: target.ScopeGlobal,
			configure: func(t *testing.T) (string, string) {
				t.Helper()
				home := filepath.Join(t.TempDir(), "home")
				t.Setenv("HOME", home)
				configRoot := filepath.Join(home, ".config", "opencode")
				if err := os.MkdirAll(configRoot, 0o700); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(configRoot, "opencode.json"), filepath.Join(configRoot, "opencode.jsonc")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			primaryPath, conflictingPath := test.configure(t)
			if err := os.WriteFile(primaryPath, []byte(`{"mcp":{"context7":{"type":"local","command":["npx"]}}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(conflictingPath, []byte(`{"mcp":{}}`), 0o600); err != nil {
				t.Fatal(err)
			}

			servers, _, skipped, err := Candidates(t.Context(), target.TargetOpenCode, test.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(servers) != 0 || len(skipped) != 1 ||
				skipped[0].LivePath != conflictingPath ||
				skipped[0].Reason != skipAlternateConfig {
				t.Fatalf("Candidates = (%#v, %#v), want one alternate-config skip", servers, skipped)
			}
		})
	}
}

func TestCandidatesReportsMalformedConfigWithoutPartialImport(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile(filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath), []byte(`{"mcpServers":`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetClaudeCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 {
		t.Fatalf("servers = %#v, want none", servers)
	}
	if len(skipped) != 1 || skipped[0].LivePath != ".mcp.json" || skipped[0].Reason != "mcp_config_malformed" {
		t.Fatalf("skipped = %#v, want malformed config", skipped)
	}
}

func TestCandidatesRetainsAuthorityForMalformedAndEmptyDocuments(t *testing.T) {
	for _, test := range []struct {
		name       string
		content    string
		wantReason string
	}{
		{name: "malformed", content: `{"mcpServers":`, wantReason: "mcp_config_malformed"},
		{name: "empty", content: `{"mcpServers":{}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			withWorkingDirectory(t, root)
			if err := os.WriteFile(aggregate.ClaudeProjectMCPConfigPath, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}

			servers, authorities, skipped, err := Candidates(t.Context(), target.TargetClaudeCode, target.ScopeProject)
			if err != nil {
				t.Fatal(err)
			}
			if len(servers) != 0 {
				t.Fatalf("servers = %#v, want none", servers)
			}
			if len(authorities) != 1 ||
				authorities[0].PrimaryPath != aggregate.ClaudeProjectMCPConfigPath ||
				authorities[0].PrimaryRevision == "" ||
				authorities[0].MaximumBytes <= 0 {
				t.Fatalf("authorities = %#v, want exact readable document authority", authorities)
			}
			if test.wantReason == "" {
				if len(skipped) != 0 {
					t.Fatalf("skipped = %#v, want none for empty document", skipped)
				}
				return
			}
			if len(skipped) != 1 || string(skipped[0].Reason) != test.wantReason {
				t.Fatalf("skipped = %#v, want %q", skipped, test.wantReason)
			}
		})
	}
}

func TestCandidatesRejectsUnpairedSurrogateWithoutPartialImport(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile(
		filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath),
		[]byte(`{"mcpServers":{"context7":{"type":"stdio","command":"node","args":["\ud800"]}}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetClaudeCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 || len(skipped) != 1 ||
		skipped[0].LivePath != aggregate.ClaudeProjectMCPConfigPath ||
		skipped[0].Reason != "mcp_config_malformed" {
		t.Fatalf("Candidates = (%#v, %#v), want one malformed config skip", servers, skipped)
	}
}

func TestCandidatesSkipsOversizedMCPDocumentWithoutPartialImport(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project MCP placement is missing")
	}
	codec, ok := aggregatecodec.Catalog().Lookup(placement.CodecContractID())
	if !ok {
		t.Fatalf("MCP codec %q is missing", placement.CodecContractID())
	}
	path := filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(codec.MaximumDocumentBytes() + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetClaudeCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 || len(skipped) != 1 ||
		skipped[0].LivePath != aggregate.ClaudeProjectMCPConfigPath ||
		skipped[0].Reason != skipTooLarge {
		t.Fatalf("Candidates = (%#v, %#v), want one document-level size skip", servers, skipped)
	}
}

func TestCandidatesStopsWhenMCPImportContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	servers, _, skipped, err := Candidates(ctx, target.TargetClaudeCode, target.ScopeProject)
	if !errors.Is(err, context.Canceled) || servers != nil || skipped != nil {
		t.Fatalf("Candidates = (%#v, %#v, %v), want context cancellation", servers, skipped, err)
	}
}

func TestCandidatesDoesNotFlattenProviderScopedSiblingMCP(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile(filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath), []byte(`{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx"}
  },
  "pluginMCPServers": {
    "context7": {"provider": "example-plugin", "command": "plugin-owned"}
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetClaudeCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none for ignored provider sibling", skipped)
	}
	if len(servers) != 1 || servers[0].ResourceName != "context7" || servers[0].Command != "npx" {
		t.Fatalf("servers = %#v, want only standalone context7", servers)
	}
}

func TestCandidatesDoesNotFlattenGlobalProviderScopedSiblingMCP(t *testing.T) {
	tests := []struct {
		name     string
		target   target.Target
		livePath string
		content  string
		wantPath string
	}{
		{
			name:     "claude global",
			target:   target.TargetClaudeCode,
			livePath: ".claude.json",
			content: `{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx"}
  },
  "projects": {
    "/repo": {
      "mcpServers": {
        "context7": {"type": "stdio", "command": "project-owned"}
      }
    }
  },
  "pluginMCPServers": {
    "context7": {"provider": "example-plugin", "command": "plugin-owned"}
  }
}`,
			wantPath: "#/mcpServers/context7",
		},
		{
			name:     "codex global",
			target:   target.TargetCodex,
			livePath: filepath.Join(".codex", "config.toml"),
			content: `
[mcp_servers.context7]
command = "npx"

[plugin_mcp_servers.context7]
provider = "example-plugin"
command = "plugin-owned"
`,
			wantPath: "#/mcp_servers/context7",
		},
		{
			name:     "opencode global",
			target:   target.TargetOpenCode,
			livePath: filepath.Join(".config", "opencode", "opencode.json"),
			content: `{
  "mcp": {
    "context7": {"type": "local", "command": ["npx"]}
  },
  "pluginMCPServers": {
    "context7": {"provider": "example-plugin", "command": "plugin-owned"}
  }
}`,
			wantPath: "#/mcp/context7",
		},
		{
			name:     "antigravity global",
			target:   target.TargetAntigravityCLI,
			livePath: filepath.Join(".gemini", "config", "mcp_config.json"),
			content: `{
  "mcpServers": {
    "context7": {"command": "npx"}
  },
  "pluginMCPServers": {
    "context7": {"provider": "example-plugin", "command": "plugin-owned"}
  }
}`,
			wantPath: "#/mcpServers/context7",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			homeDir := filepath.Join(tempDir, "home")
			t.Setenv("HOME", homeDir)
			livePath := filepath.Join(homeDir, test.livePath)
			if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(livePath, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}

			servers, _, skipped, err := Candidates(t.Context(), test.target, target.ScopeGlobal)
			if err != nil {
				t.Fatal(err)
			}
			if len(skipped) != 0 {
				t.Fatalf("skipped = %#v, want ignored provider sibling without importing or rejecting it", skipped)
			}
			if len(servers) != 1 ||
				servers[0].ResourceName != "context7" ||
				servers[0].Target != test.target ||
				servers[0].Scope != target.ScopeGlobal ||
				servers[0].LivePath() != livePath+test.wantPath ||
				servers[0].Command != "npx" {
				t.Fatalf("servers = %#v, want only standalone global context7", servers)
			}
		})
	}
}

func TestCandidatesReportsGlobalMalformedConfigWithoutPartialImport(t *testing.T) {
	tests := []struct {
		name     string
		target   target.Target
		livePath string
		content  string
	}{
		{
			name:     "claude global",
			target:   target.TargetClaudeCode,
			livePath: ".claude.json",
			content:  `{"mcpServers":`,
		},
		{
			name:     "codex global",
			target:   target.TargetCodex,
			livePath: filepath.Join(".codex", "config.toml"),
			content:  `mcp_servers = [`,
		},
		{
			name:     "opencode global",
			target:   target.TargetOpenCode,
			livePath: filepath.Join(".config", "opencode", "opencode.json"),
			content:  `{"mcp":`,
		},
		{
			name:     "antigravity global",
			target:   target.TargetAntigravityCLI,
			livePath: filepath.Join(".gemini", "config", "mcp_config.json"),
			content:  `{"mcpServers":`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			homeDir := filepath.Join(tempDir, "home")
			t.Setenv("HOME", homeDir)
			livePath := filepath.Join(homeDir, test.livePath)
			if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(livePath, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}

			servers, _, skipped, err := Candidates(t.Context(), test.target, target.ScopeGlobal)
			if err != nil {
				t.Fatal(err)
			}
			if len(servers) != 0 {
				t.Fatalf("servers = %#v, want no partial import from malformed global config", servers)
			}
			if len(skipped) != 1 || skipped[0].LivePath != livePath || skipped[0].Reason != "mcp_config_malformed" {
				t.Fatalf("skipped = %#v, want malformed global config skip", skipped)
			}
		})
	}
}

func TestCandidatesReportsCodexStructureLimitAsMalformedWithoutPartialImport(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	livePath := filepath.Join(homeDir, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "root = " + strings.Repeat("{ k = ", 64) + "1" + strings.Repeat(" }", 64) + "\n"
	if err := os.WriteFile(livePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetCodex, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 || len(skipped) != 1 ||
		skipped[0].LivePath != livePath || skipped[0].Reason != "mcp_config_malformed" {
		t.Fatalf("Candidates = (%#v, %#v), want one malformed Codex structure-limit skip", servers, skipped)
	}
}

func TestCandidatesRejectsDuplicateServerKeysWithoutPartialImport(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile(filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath), []byte(`{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx"},
    "context7": {"type": "stdio", "command": "node"}
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, _, skipped, err := Candidates(t.Context(), target.TargetClaudeCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 {
		t.Fatalf("servers = %#v, want no partial import", servers)
	}
	if len(skipped) != 1 || skipped[0].LivePath != ".mcp.json" || skipped[0].Reason != "mcp_config_malformed" {
		t.Fatalf("skipped = %#v, want duplicate-key config rejection", skipped)
	}
}

func TestMCPConfigPathUsesCanonicalScopeAwareOutputGrammar(t *testing.T) {
	projectDestination, err := output.Parse(".mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	projectPath, err := mcpConfigPath(projectDestination, target.ScopeProject)
	if err != nil {
		t.Fatalf("mcpConfigPath(project) error = %v", err)
	}
	if projectPath != ".mcp.json" {
		t.Fatalf("mcpConfigPath(project) = %q, want .mcp.json", projectPath)
	}

	if _, err := mcpConfigPath(projectDestination, target.ScopeGlobal); err == nil {
		t.Fatal("mcpConfigPath(project destination, global scope) succeeded, want error")
	}

	dataDestination, err := output.Parse("@data/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = mcpConfigPath(dataDestination, target.ScopeGlobal)
	if err == nil || !strings.Contains(err.Error(), "managed data root is required") {
		t.Fatalf("mcpConfigPath(data) error = %v, want explicit unavailable data root", err)
	}
}

func withWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}
