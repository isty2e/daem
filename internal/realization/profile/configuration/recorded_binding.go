package configuration

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// Retained rules do not consult mutable placement/catalog selection. Extending
// one of these meanings requires a new policy revision.
func recordedAliasBinding(input AliasInput, policy string) (AliasBinding, error) {
	for _, rule := range []struct {
		policy, alias, identity, locator, child string
		target                                  target.Target
		scope                                   target.Scope
		point                                   bool
	}{
		{"pi-mcp-carrier-v1", aggregate.PiGlobalMCPConfigPath, "pi-agent", "pi-agent-location-v1", "mcp.json", target.TargetPi, target.ScopeGlobal, false},
		{"codex-mcp-carrier-v1", aggregate.CodexGlobalMCPConfigPath, "codex-home", "codex-home-location-v1", "config.toml", target.TargetCodex, target.ScopeGlobal, false},
		{"claude-account-carrier-v1", aggregate.ClaudeGlobalMCPConfigPath, "claude-account", "claude-account-location-v1", ".claude.json", target.TargetClaudeCode, target.ScopeGlobal, false},
		{"claude-account-carrier-v2", aggregate.ClaudeGlobalMCPConfigPath, "claude-account", "claude-account-point-v2", "", target.TargetClaudeCode, target.ScopeGlobal, true},
		{"claude-owned-mcp-source-v1", aggregate.ClaudeGlobalMCPConfigPath, "claude-account-owned-source", "claude-owned-source-point-v1", "", target.TargetClaudeCode, target.ScopeGlobal, true},
		{"claude-mcp-carrier-v1", aggregate.ClaudeProjectMCPConfigPath, "project", "selected-project-v1", ".mcp.json", target.TargetClaudeCode, target.ScopeProject, false},
		{"antigravity-mcp-carrier-v1", aggregate.AntigravityGlobalMCPConfigPath, "antigravity-shared", "antigravity-shared-location-v1", "mcp_config.json", target.TargetAntigravityCLI, target.ScopeGlobal, false},
		{"antigravity-mcp-carrier-v2", aggregate.AntigravityGlobalMCPConfigPath, "antigravity-shared", "antigravity-shared-point-v2", "", target.TargetAntigravityCLI, target.ScopeGlobal, true},
	} {
		if policy != rule.policy || input.Target != rule.target || input.Scope != rule.scope || input.Destination.String() != rule.alias {
			continue
		}
		reference, err := output.NewRootReference(rule.identity, rule.locator)
		if err != nil {
			return AliasBinding{}, err
		}
		if rule.point {
			location, err := NewPointAliasLocation(reference)
			return AliasBinding{location: location, policy: policy}, err
		}
		return newAliasBinding(rootFact{rule.identity, rule.locator}, rule.child, policy)
	}
	var selected target.Target
	var root rootFact
	var role output.RootRole
	var prefix string
	switch policy {
	case "pi-native-v1":
		selected, root, role = target.TargetPi, rootFact{"pi-agent", "pi-agent-location-v1"}, output.RootPiAgent
	case "codex-native-v1":
		selected, root, role = target.TargetCodex, rootFact{"codex-home", "codex-home-location-v1"}, output.RootCodexHome
	case "claude-native-v1":
		selected, root, role, prefix = target.TargetClaudeCode, rootFact{"claude-preferences", "claude-preferences-location-v1"}, output.RootHome, ".claude/"
	case "opencode-native-v1":
		selected, root, role, prefix = target.TargetOpenCode, rootFact{"opencode-configuration", "opencode-location-v1"}, output.RootHome, ".config/opencode/"
	case "antigravity-native-v1":
		selected, root, role, prefix = target.TargetAntigravityCLI, rootFact{"antigravity-preferences", "antigravity-preferences-location-v1"}, output.RootHome, ".gemini/antigravity-cli/"
	default:
		return AliasBinding{}, fmt.Errorf("%w: retained document policy is unknown", ErrDocumentPath)
	}
	if input.Target != selected {
		return AliasBinding{}, fmt.Errorf("%w: retained policy target differs", ErrDocumentPath)
	}
	if input.Scope == target.ScopeProject && selected != target.TargetAntigravityCLI && input.Destination.RootRole() == output.RootProject {
		return newAliasBinding(rootFact{"project", "selected-project-v1"}, input.Destination.RelativePath(), policy)
	}
	if input.Scope != target.ScopeGlobal || input.Destination.RootRole() != role {
		return AliasBinding{}, fmt.Errorf("%w: retained policy scope/root differs", ErrDocumentPath)
	}
	child, matched := strings.CutPrefix(input.Destination.RelativePath(), prefix)
	if !matched {
		return AliasBinding{}, fmt.Errorf("%w: retained alias is outside its layout", ErrDocumentPath)
	}
	return newAliasBinding(root, child, policy)
}

// OwnedMCPSourceBinding describes the qualified historical endpoint, never a
// forward location policy or authority inferred from a persisted selector.
func OwnedMCPSourceBinding(input AliasInput) (AliasBinding, error) {
	return recordedAliasBinding(input, "claude-owned-mcp-source-v1")
}
