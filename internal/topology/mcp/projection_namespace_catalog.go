package mcp

import "github.com/isty2e/daem/internal/target"

type projectionNamespaceRow struct {
	target    target.Target
	scope     target.Scope
	namespace string
}

var projectionNamespaceCatalog = [...]projectionNamespaceRow{
	{target: target.TargetClaudeCode, scope: target.ScopeProject, namespace: "claude-code.project.mcp-server"},
	{target: target.TargetClaudeCode, scope: target.ScopeGlobal, namespace: "claude-code.global.mcp-server"},
	{target: target.TargetAntigravityCLI, scope: target.ScopeGlobal, namespace: "antigravity-cli.global.mcp-server"},
	{target: target.TargetOpenCode, scope: target.ScopeProject, namespace: "opencode.project.mcp-server"},
	{target: target.TargetOpenCode, scope: target.ScopeGlobal, namespace: "opencode.global.mcp-server"},
	{target: target.TargetCodex, scope: target.ScopeProject, namespace: "codex.project.mcp-server"},
	{target: target.TargetCodex, scope: target.ScopeGlobal, namespace: "codex.global.mcp-server"},
	{target: target.TargetPi, scope: target.ScopeProject, namespace: "pi.project.mcp-server"},
	{target: target.TargetPi, scope: target.ScopeGlobal, namespace: "pi.global.mcp-server"},
}
