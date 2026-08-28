package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestCandidatesUseSafeParentSubjectAcrossEveryImportRoute(t *testing.T) {
	const canary = "REJECTION_SUBJECT_LEAK_CANARY"
	tests := []struct {
		name        string
		target      target.Target
		scope       target.Scope
		projectPath string
		globalPath  string
		content     string
		prefix      string
	}{
		{name: "Claude project", target: target.TargetClaudeCode, scope: target.ScopeProject, projectPath: aggregate.ClaudeProjectMCPConfigPath, content: `{"mcpServers":{"token=` + canary + `/path":{"type":"http","command":"node"},"valid":{"type":"stdio","command":"node"}}}`, prefix: "/mcpServers"},
		{name: "Claude global", target: target.TargetClaudeCode, scope: target.ScopeGlobal, globalPath: ".claude.json", content: `{"mcpServers":{"token=` + canary + `/path":{"type":"http","command":"node"},"valid":{"type":"stdio","command":"node"}}}`, prefix: "/mcpServers"},
		{name: "OpenCode project", target: target.TargetOpenCode, scope: target.ScopeProject, projectPath: aggregate.OpenCodeProjectMCPConfigPath, content: `{"mcp":{"token=` + canary + `/path":{"type":"remote","command":["node"]},"valid":{"type":"local","command":["node"]}}}`, prefix: "/mcp"},
		{name: "OpenCode global", target: target.TargetOpenCode, scope: target.ScopeGlobal, globalPath: filepath.Join(".config", "opencode", "opencode.json"), content: `{"mcp":{"token=` + canary + `/path":{"type":"remote","command":["node"]},"valid":{"type":"local","command":["node"]}}}`, prefix: "/mcp"},
		{name: "Codex project", target: target.TargetCodex, scope: target.ScopeProject, projectPath: aggregate.CodexProjectMCPConfigPath, content: `[mcp_servers."token=` + canary + `/path"]
command = "node"

[mcp_servers.valid]
command = "node"
`, prefix: "/mcp_servers"},
		{name: "Codex global", target: target.TargetCodex, scope: target.ScopeGlobal, globalPath: filepath.Join(".codex", "config.toml"), content: `[mcp_servers."token=` + canary + `/path"]
command = "node"

[mcp_servers.valid]
command = "node"
`, prefix: "/mcp_servers"},
		{name: "Antigravity global", target: target.TargetAntigravityCLI, scope: target.ScopeGlobal, globalPath: filepath.Join(".gemini", "config", "mcp_config.json"), content: `{"mcpServers":{"token=` + canary + `/path":{"serverUrl":"https://example.invalid"},"valid":{"command":"node"}}}`, prefix: "/mcpServers"},
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

			servers, authorities, skipped, err := collectCandidates(t.Context(), test.target, test.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(servers) != 1 || servers[0].ResourceName != "valid" || len(authorities) != 1 {
				t.Fatalf("Candidates = (%#v, %#v, %#v)", servers, authorities, skipped)
			}
			wantLivePath := livePath + "#" + test.prefix
			if len(skipped) != 1 || skipped[0].LivePath != wantLivePath || skipped[0].Reason != "projection_equivalence_undefined" {
				t.Fatalf("skipped = %#v, want safe parent %q", skipped, wantLivePath)
			}
			if strings.Contains(skipped[0].LivePath, canary) {
				t.Fatalf("safe rejection subject disclosed raw key: %q", skipped[0].LivePath)
			}
		})
	}
}
