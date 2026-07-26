package mcp

import (
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestProjectionNamespaceCatalogIsCompleteAndUnique(t *testing.T) {
	want := map[struct {
		target target.Target
		scope  target.Scope
	}]string{
		{target: target.TargetClaudeCode, scope: target.ScopeProject}:    "claude-code.project.mcp-server",
		{target: target.TargetClaudeCode, scope: target.ScopeGlobal}:     "claude-code.global.mcp-server",
		{target: target.TargetAntigravityCLI, scope: target.ScopeGlobal}: "antigravity-cli.global.mcp-server",
		{target: target.TargetOpenCode, scope: target.ScopeProject}:      "opencode.project.mcp-server",
		{target: target.TargetOpenCode, scope: target.ScopeGlobal}:       "opencode.global.mcp-server",
		{target: target.TargetCodex, scope: target.ScopeProject}:         "codex.project.mcp-server",
		{target: target.TargetCodex, scope: target.ScopeGlobal}:          "codex.global.mcp-server",
	}
	if len(projectionNamespaceCatalog) != len(want) {
		t.Fatalf("catalog rows = %d, want %d", len(projectionNamespaceCatalog), len(want))
	}

	seenNamespaces := make(map[string]struct{}, len(projectionNamespaceCatalog))
	for _, row := range projectionNamespaceCatalog {
		key := struct {
			target target.Target
			scope  target.Scope
		}{target: row.target, scope: row.scope}
		if got, ok := want[key]; !ok || got != row.namespace {
			t.Fatalf("unexpected catalog row: %#v", row)
		}
		if _, exists := seenNamespaces[row.namespace]; exists {
			t.Fatalf("duplicate projection namespace %q", row.namespace)
		}
		seenNamespaces[row.namespace] = struct{}{}
	}
}
