package mcp

import (
	"fmt"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// ProjectionSubject constructs the canonical structural identity for one MCP
// server projection at target and scope.
func ProjectionSubject(selectedTarget target.Target, scope target.Scope, serverName string) (topology.SubjectID, error) {
	namespace, err := projectionNamespace(selectedTarget, scope)
	if err != nil {
		return topology.SubjectID{}, err
	}
	subject, err := topology.NewSubjectID(topology.SubjectProjection, namespace, serverName)
	if err != nil {
		return topology.SubjectID{}, fmt.Errorf("MCP projection server name: %w", err)
	}
	return subject, nil
}

// IsProjectionFor reports whether subject is the canonical MCP projection
// identity family for target and scope.
func IsProjectionFor(selectedTarget target.Target, scope target.Scope, subject topology.SubjectID) bool {
	namespace, err := projectionNamespace(selectedTarget, scope)
	if err != nil || subject.Validate() != nil {
		return false
	}
	return subject.Kind() == topology.SubjectProjection && subject.Namespace() == namespace
}

// ServerID returns the family-local server name carried by an MCP projection
// subject.
func ServerID(subject topology.SubjectID) (string, bool) {
	if subject.Validate() != nil || subject.Kind() != topology.SubjectProjection {
		return "", false
	}
	for _, row := range projectionNamespaceCatalog {
		if subject.Namespace() == row.namespace {
			return subject.Key(), true
		}
	}
	return "", false
}

func projectionNamespace(selectedTarget target.Target, scope target.Scope) (string, error) {
	if _, err := target.ParseTarget(string(selectedTarget)); err != nil {
		return "", err
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return "", err
	}
	for _, row := range projectionNamespaceCatalog {
		if row.target == selectedTarget && row.scope == scope {
			return row.namespace, nil
		}
	}
	for _, row := range projectionNamespaceCatalog {
		if row.target == selectedTarget {
			return "", fmt.Errorf("unsupported MCP scope %q", scope)
		}
	}
	return "", fmt.Errorf("unsupported MCP target %q", selectedTarget)
}
