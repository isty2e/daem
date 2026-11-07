package configuration

import (
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type scopeKey struct {
	target target.Target
	scope  target.Scope
}

type rootFact struct {
	identity string
	locator  string
}

type nativeLayout struct {
	root           rootFact
	aliasRoot      output.RootRole
	aliasDirectory string
	relativePrefix string
	policy         string
	rootFiles      []string
	subtree        string
	observations   observationSet
}

var (
	projectRoot       = rootFact{"project", "selected-project-v1"}
	piRoot            = rootFact{"pi-agent", "pi-agent-location-v1"}
	codexRoot         = rootFact{"codex-home", "codex-home-location-v1"}
	claudePreferences = rootFact{"claude-preferences", "claude-preferences-location-v1"}
	claudeAccount     = rootFact{"claude-account", "claude-account-location-v1"}
	openCodeRoot      = rootFact{"opencode-configuration", "opencode-location-v1"}
	antigravityRoot   = rootFact{"antigravity-preferences", "antigravity-preferences-location-v1"}
	antigravityShared = rootFact{"antigravity-shared", "antigravity-shared-location-v1"}
)

var nativeLayouts = map[scopeKey]nativeLayout{
	{target.TargetPi, target.ScopeGlobal}: {
		root: piRoot, aliasRoot: output.RootPiAgent, policy: "pi-native-v1", observations: piLoader,
	},
	{target.TargetPi, target.ScopeProject}: {
		root: projectRoot, aliasRoot: output.RootProject, aliasDirectory: ".pi/", relativePrefix: ".pi/", policy: "pi-native-v1", observations: piLoader,
	},
	{target.TargetCodex, target.ScopeGlobal}: {
		root: codexRoot, aliasRoot: output.RootCodexHome, policy: "codex-native-v1", observations: codexRuntime,
	},
	{target.TargetCodex, target.ScopeProject}: {
		root: projectRoot, aliasRoot: output.RootProject, aliasDirectory: ".codex/", relativePrefix: ".codex/", policy: "codex-native-v1", observations: codexRuntime,
	},
	{target.TargetClaudeCode, target.ScopeGlobal}: {
		root: claudePreferences, aliasRoot: output.RootHome, aliasDirectory: ".claude/", policy: "claude-native-v1",
	},
	{target.TargetClaudeCode, target.ScopeProject}: {
		root: projectRoot, aliasRoot: output.RootProject, aliasDirectory: ".claude/", relativePrefix: ".claude/", policy: "claude-native-v1", observations: claudeLocation,
	},
	{target.TargetOpenCode, target.ScopeGlobal}: {
		root: openCodeRoot, aliasRoot: output.RootHome, aliasDirectory: ".config/opencode/", policy: "opencode-native-v1",
	},
	{target.TargetOpenCode, target.ScopeProject}: {
		root: projectRoot, aliasRoot: output.RootProject, policy: "opencode-native-v1", subtree: ".opencode/",
		rootFiles: []string{"opencode.json", "opencode.jsonc", "tui.json", "tui.jsonc"},
	},
	{target.TargetAntigravityCLI, target.ScopeGlobal}: {
		root: antigravityRoot, aliasRoot: output.RootHome, aliasDirectory: ".gemini/antigravity-cli/", policy: "antigravity-native-v1",
	},
}

var absentNativeScopes = map[scopeKey]string{
	{target.TargetAntigravityCLI, target.ScopeProject}: "the CLI publishes user preferences, not a project preferences layer",
}

func selectLayout(selected target.Target, scope target.Scope) (nativeLayout, error) {
	if _, err := target.ParseTarget(string(selected)); err != nil {
		return nativeLayout{}, err
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return nativeLayout{}, err
	}
	key := scopeKey{selected, scope}
	if reason, absent := absentNativeScopes[key]; absent {
		return nativeLayout{}, fmt.Errorf("%w: %s/%s: %s", ErrNoNativeSurface, selected, scope, reason)
	}
	layout, present := nativeLayouts[key]
	if !present {
		return nativeLayout{}, fmt.Errorf("native configuration facts are missing for %s/%s", selected, scope)
	}
	return layout, nil
}

func (layout nativeLayout) contains(nativePath string) bool {
	return layout.subtree == "" || strings.HasPrefix(nativePath, layout.subtree) || slices.Contains(layout.rootFiles, nativePath)
}

type dedicatedAliasFact struct {
	target       target.Target
	scope        target.Scope
	alias        string
	root         rootFact
	relative     string
	policy       string
	observations observationSet
}

// These dedicated aliases are outside generic native layouts. This is not the
// set of ordinary writers: an ordinary alias may also have a native spelling.
var dedicatedDocumentAliases = []dedicatedAliasFact{
	{target.TargetPi, target.ScopeGlobal, aggregate.PiGlobalMCPConfigPath, piRoot, "mcp.json", "pi-mcp-carrier-v1", piLoader},
	{target.TargetCodex, target.ScopeGlobal, aggregate.CodexGlobalMCPConfigPath, codexRoot, "config.toml", "codex-mcp-carrier-v1", codexRuntime},
	{target.TargetClaudeCode, target.ScopeGlobal, aggregate.ClaudeGlobalMCPConfigPath, claudeAccount, ".claude.json", "claude-account-carrier-v1", 0},
	{target.TargetClaudeCode, target.ScopeProject, aggregate.ClaudeProjectMCPConfigPath, projectRoot, ".mcp.json", "claude-mcp-carrier-v1", 0},
	{target.TargetAntigravityCLI, target.ScopeGlobal, aggregate.AntigravityGlobalMCPConfigPath, antigravityShared, "mcp_config.json", "antigravity-mcp-carrier-v1", 0},
}

type documentRule struct {
	target        target.Target
	scope         target.Scope
	path          string
	namedProfile  bool
	dedicatedOnly bool
	grammar       Grammar
	class         DocumentClass
	alternate     string
	observations  observationSet
}

var documentRules = []documentRule{
	{target: target.TargetPi, path: "settings.json", grammar: JSON, class: SharedConfiguration},
	{target: target.TargetPi, scope: target.ScopeGlobal, path: "models.json", grammar: JSONLineComments, class: ModelCatalog},
	{target: target.TargetPi, path: "mcp.json", grammar: JSON, class: SharedConfiguration},
	{target: target.TargetCodex, path: "config.toml", grammar: TOML, class: SharedConfiguration},
	{target: target.TargetCodex, scope: target.ScopeGlobal, namedProfile: true, grammar: TOML, class: SharedConfiguration},
	{target: target.TargetClaudeCode, path: "settings.json", grammar: JSON, class: SharedConfiguration},
	{target: target.TargetClaudeCode, scope: target.ScopeProject, path: "settings.local.json", grammar: JSON, class: SharedConfiguration},
	{target: target.TargetClaudeCode, scope: target.ScopeGlobal, path: ".claude.json", dedicatedOnly: true, grammar: JSON, class: SharedConfiguration},
	{target: target.TargetClaudeCode, scope: target.ScopeProject, path: ".mcp.json", dedicatedOnly: true, grammar: JSON, class: SharedConfiguration},
	{target: target.TargetOpenCode, path: "opencode.json", grammar: JSONC, class: SharedConfiguration, alternate: "opencode.jsonc", observations: openCodeCandidates},
	{target: target.TargetOpenCode, path: "opencode.jsonc", grammar: JSONC, class: SharedConfiguration, alternate: "opencode.json", observations: openCodeCandidates},
	{target: target.TargetOpenCode, path: "tui.json", grammar: JSONC, class: SharedConfiguration, alternate: "tui.jsonc", observations: openCodeCandidates},
	{target: target.TargetOpenCode, path: "tui.jsonc", grammar: JSONC, class: SharedConfiguration, alternate: "tui.json", observations: openCodeCandidates},
	{target: target.TargetOpenCode, scope: target.ScopeProject, path: ".opencode/opencode.json", grammar: JSONC, class: SharedConfiguration, alternate: ".opencode/opencode.jsonc", observations: openCodeCandidates},
	{target: target.TargetOpenCode, scope: target.ScopeProject, path: ".opencode/opencode.jsonc", grammar: JSONC, class: SharedConfiguration, alternate: ".opencode/opencode.json", observations: openCodeCandidates},
	{target: target.TargetOpenCode, scope: target.ScopeProject, path: ".opencode/tui.json", grammar: JSONC, class: SharedConfiguration, alternate: ".opencode/tui.jsonc", observations: openCodeCandidates},
	{target: target.TargetOpenCode, scope: target.ScopeProject, path: ".opencode/tui.jsonc", grammar: JSONC, class: SharedConfiguration, alternate: ".opencode/tui.json", observations: openCodeCandidates},
	{target: target.TargetAntigravityCLI, path: "settings.json", grammar: JSON, class: Preferences},
	{target: target.TargetAntigravityCLI, path: "keybindings.json", grammar: JSON, class: Preferences},
	{target: target.TargetAntigravityCLI, scope: target.ScopeGlobal, path: "mcp_config.json", dedicatedOnly: true, grammar: JSON, class: SharedConfiguration},
}

func documentRuleFor(selected target.Target, scope target.Scope, nativePath string, outsideNativeLayout bool) (documentRule, error) {
	result := documentRule{class: GenericConfiguration}
	matched := false
	for _, rule := range documentRules {
		if rule.target != selected || rule.scope != "" && rule.scope != scope || rule.dedicatedOnly && !outsideNativeLayout {
			continue
		}
		if rule.namedProfile {
			first, _, nested := strings.Cut(nativePath, "/")
			if nested && namedProfilePath(first) {
				return documentRule{}, fmt.Errorf("%w: %q is beneath known document %q", ErrDocumentPath, nativePath, first)
			}
			if !namedProfilePath(nativePath) {
				continue
			}
		} else {
			if strings.HasPrefix(nativePath, rule.path+"/") {
				return documentRule{}, fmt.Errorf("%w: %q is beneath known document %q", ErrDocumentPath, nativePath, rule.path)
			}
			if rule.path != nativePath {
				continue
			}
		}
		if matched {
			return documentRule{}, fmt.Errorf("ambiguous native document facts for %s/%s %q", selected, scope, nativePath)
		}
		result, matched = rule, true
	}
	if outsideNativeLayout && !matched {
		return documentRule{}, fmt.Errorf("dedicated outside-layout alias has no document grammar contract")
	}
	return result, nil
}

func namedProfilePath(nativePath string) bool {
	name, matched := strings.CutSuffix(nativePath, ".config.toml")
	return matched && name != "" && strings.IndexFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) < 0
}

type observationSet uint8

const (
	piLoader observationSet = 1 << iota
	codexRuntime
	claudeLocation
	openCodeCandidates
)

func (set observationSet) values() []Observation {
	values := []Observation{ReferentAliases, InstalledClaims}
	for _, entry := range []struct {
		flag  observationSet
		value Observation
	}{
		{piLoader, PiLoader},
		{codexRuntime, CodexRuntimeLocations},
		{claudeLocation, ClaudeProjectLocation},
		{openCodeCandidates, OpenCodeCandidates},
	} {
		if set&entry.flag != 0 {
			values = append(values, entry.value)
		}
	}
	return values
}
