package mcp

import "github.com/isty2e/daem/internal/target"

type projectionNamespaceRow struct {
	target    target.Target
	scope     target.Scope
	namespace string
}

// ProjectionNamespace is the owner-local MCP projection namespace for one
// target and scope. It does not construct SubjectID values.
type ProjectionNamespace struct {
	target    target.Target
	scope     target.Scope
	namespace string
}

// Target returns the host target for this namespace contract.
func (row ProjectionNamespace) Target() target.Target {
	return row.target
}

// Scope returns the manifest scope for this namespace contract.
func (row ProjectionNamespace) Scope() target.Scope {
	return row.scope
}

// Namespace returns the canonical topology namespace token.
func (row ProjectionNamespace) Namespace() string {
	return row.namespace
}

// ImplementedProjectionNamespaces returns implemented MCP projection
// namespaces in stable catalog order.
func ImplementedProjectionNamespaces() []ProjectionNamespace {
	rows := make([]ProjectionNamespace, 0, len(projectionNamespaceCatalog))
	for _, row := range projectionNamespaceCatalog {
		rows = append(rows, ProjectionNamespace{
			target:    row.target,
			scope:     row.scope,
			namespace: row.namespace,
		})
	}
	return rows
}

// Namespace returns the canonical MCP projection namespace for target and scope.
func Namespace(selectedTarget target.Target, scope target.Scope) (string, error) {
	return projectionNamespace(selectedTarget, scope)
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
