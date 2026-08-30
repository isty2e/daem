package authoring

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationnormalize "github.com/isty2e/daem/internal/declaration/normalize"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/hostsurface/catalog"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func canonicalMCPServerAuthoring(server declarationcodec.MCPServer) (desiredmcp.Server, desiredmcp.Binding, error) {
	canonical, binding, err := declarationnormalize.ExplicitMCPServer(server)
	if err != nil {
		return desiredmcp.Server{}, desiredmcp.Binding{}, err
	}
	if _, err := aggregate.MCPPlacementForBinding(binding); err != nil {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp-server: %w", err)
	}
	return canonical, binding, nil
}

func validateCanonicalMCPServerAuthoring(server declarationcodec.MCPServer) error {
	_, _, err := canonicalMCPServerAuthoring(server)
	return err
}

func addMCPAuthoringTargetScope(rawTargets []string, rawScope string, header declaration.ManifestHeader, origin daempaths.ManifestOrigin) ([]string, string, error) {
	scopeInput := singleRowAuthoringScope(rawScope, origin)
	if len(rawTargets) > 1 {
		return nil, "", fmt.Errorf("mcp-server authoring accepts at most one distinct --target")
	}
	if len(rawTargets) == 1 {
		targets, err := addMCPAuthoringTargets(rawTargets)
		if err != nil {
			return nil, "", err
		}
		scope, err := addMCPAuthoringScope(targets[0], scopeInput)
		if err != nil {
			return nil, "", err
		}
		return targets, scope, nil
	}

	candidates := make([]string, 0, len(header.Targets))
	seen := make(map[string]struct{}, len(header.Targets))
	for _, targetValue := range header.Targets {
		if _, exists := seen[targetValue]; exists {
			continue
		}
		if _, err := addMCPAuthoringTargets([]string{targetValue}); err != nil {
			continue
		}
		if _, err := addMCPAuthoringScope(targetValue, scopeInput); err != nil {
			continue
		}
		seen[targetValue] = struct{}{}
		candidates = append(candidates, targetValue)
	}
	if len(candidates) == 1 {
		scope, err := addMCPAuthoringScope(candidates[0], scopeInput)
		if err != nil {
			return nil, "", err
		}
		return candidates, scope, nil
	}
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("mcp-server target cannot be inferred from manifest targets; pass --target and, for global authority, --scope global")
	}
	return nil, "", fmt.Errorf("mcp-server target is ambiguous across manifest targets %s; pass one --target", strings.Join(candidates, ", "))
}

func singleRowAuthoringScope(rawScope string, origin daempaths.ManifestOrigin) string {
	if strings.TrimSpace(rawScope) != "" {
		return strings.TrimSpace(rawScope)
	}
	if origin == daempaths.ManifestOriginUserDefault {
		return string(target.ScopeGlobal)
	}
	return ""
}

func addMCPAuthoringTargets(rawTargets []string) ([]string, error) {
	if len(rawTargets) == 0 {
		return nil, fmt.Errorf("mcp-server target is required when it cannot be inferred")
	}
	if len(rawTargets) != 1 {
		return nil, fmt.Errorf("mcp-server authoring accepts exactly one --target")
	}
	selectedTarget, err := target.ParseTarget(rawTargets[0])
	if err != nil || !catalog.Product().HasMCPTarget(selectedTarget) {
		return nil, fmt.Errorf("mcp-server authoring supports only %s", mcpAuthoringTargetOptions())
	}
	return []string{string(selectedTarget)}, nil
}

func normalizedMCPRemoveSelector(rawTargets []string, rawScope string) ([]string, string, error) {
	if len(rawTargets) > 1 {
		return nil, "", fmt.Errorf("mcp-server removal accepts at most one distinct --target")
	}
	if len(rawTargets) == 1 {
		if _, err := addMCPAuthoringTargets(rawTargets); err != nil {
			return nil, "", err
		}
	}
	scope := strings.TrimSpace(rawScope)
	if scope != "" {
		if _, err := target.ParseScope(scope); err != nil {
			return nil, "", err
		}
	}
	return append([]string(nil), rawTargets...), scope, nil
}

func addMCPAuthoringScope(selectedTarget string, rawScope string) (string, error) {
	parsedTarget, err := target.ParseTarget(selectedTarget)
	if err != nil || !catalog.Product().HasMCPTarget(parsedTarget) {
		return "", fmt.Errorf("mcp-server authoring supports only %s", mcpAuthoringTargetOptions())
	}
	scope := strings.TrimSpace(rawScope)
	if scope == "" {
		if _, ok := catalog.Product().LookupMCP(parsedTarget, target.ScopeProject); ok {
			return string(target.ScopeProject), nil
		}
		return "", fmt.Errorf("mcp-server authoring requires --scope %s for --target %s", target.ScopeGlobal, parsedTarget)
	}
	parsedScope, err := target.ParseScope(scope)
	if err == nil {
		if _, ok := catalog.Product().LookupMCP(parsedTarget, parsedScope); ok {
			return string(parsedScope), nil
		}
	}
	return "", fmt.Errorf(
		"mcp-server authoring supports %s for --target %s",
		mcpAuthoringScopeOptions(parsedTarget),
		parsedTarget,
	)
}

func validateAddMCPAuthoringShape(selectedTarget string, selectedScope string, request AddMCPServerRequest) error {
	parsedTarget, err := target.ParseTarget(selectedTarget)
	if err != nil {
		return err
	}
	parsedScope, err := target.ParseScope(selectedScope)
	if err != nil {
		return err
	}
	view, ok := catalog.Product().LookupMCP(parsedTarget, parsedScope)
	if !ok {
		return fmt.Errorf("mcp-server authoring does not implement target/scope %s/%s", parsedTarget, parsedScope)
	}
	placement := view.Placement()
	if len(request.Env) == 0 || placement.EnvReferenceContract().Supported() {
		return nil
	}
	if mcpAuthoringTargetHasEnvPlacement(parsedTarget) {
		return fmt.Errorf("mcp-server authoring does not support --env for --target %s --scope %s", parsedTarget, parsedScope)
	}
	return fmt.Errorf("mcp-server authoring does not support --env for --target %s", parsedTarget)
}

func mcpAuthoringTargetOptions() string {
	values := make([]string, 0)
	seen := make(map[target.Target]struct{})
	for _, view := range catalog.Product().MCPInOwnerOrder() {
		selected := view.Key().Target()
		if _, exists := seen[selected]; exists {
			continue
		}
		seen[selected] = struct{}{}
		values = append(values, "--target "+string(selected))
	}
	if len(values) == 0 {
		return ""
	}
	if len(values) == 1 {
		return values[0]
	}
	return strings.Join(values[:len(values)-1], ", ") + ", or " + values[len(values)-1]
}

func mcpAuthoringScopeOptions(selectedTarget target.Target) string {
	values := make([]string, 0, 2)
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		if _, ok := catalog.Product().LookupMCP(selectedTarget, scope); ok {
			values = append(values, "--scope "+string(scope))
		}
	}
	return strings.Join(values, " or ")
}

func mcpAuthoringTargetHasEnvPlacement(selectedTarget target.Target) bool {
	for _, view := range catalog.Product().Surfaces() {
		if view.Key().Target() == selectedTarget && view.Placement().EnvReferenceContract().Supported() {
			return true
		}
	}
	return false
}
