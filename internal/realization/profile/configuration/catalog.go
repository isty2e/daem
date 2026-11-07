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
	legacyHomeDir  string
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
		root: piRoot, aliasRoot: output.RootPiAgent, legacyHomeDir: ".pi/agent/", policy: "pi-native-v1", observations: piLoader,
	},
	{target.TargetPi, target.ScopeProject}: {
		root: projectRoot, aliasRoot: output.RootProject, aliasDirectory: ".pi/", relativePrefix: ".pi/", policy: "pi-native-v1", observations: piLoader,
	},
	{target.TargetCodex, target.ScopeGlobal}: {
		root: codexRoot, aliasRoot: output.RootCodexHome, legacyHomeDir: ".codex/", policy: "codex-native-v1", observations: codexRuntime,
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

func (layout nativeLayout) nativePathFor(destination output.Destination) (string, bool) {
	if destination.RootRole() != layout.aliasRoot {
		return "", false
	}
	return strings.CutPrefix(destination.RelativePath(), layout.aliasDirectory)
}

// Legacy owner aliases project reservations, not generic alias admission or
// physical agreement with the selected root.
func (layout nativeLayout) reservationPathFor(destination output.Destination) (string, bool) {
	nativePath, matched := layout.nativePathFor(destination)
	if !matched && layout.legacyHomeDir != "" && destination.RootRole() == output.RootHome {
		nativePath, matched = strings.CutPrefix(destination.RelativePath(), layout.legacyHomeDir)
	}
	return nativePath, matched && nativePath != "" && layout.contains(nativePath)
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
	slots         dedicatedSlotSet
}

var documentRules = []documentRule{
	{target: target.TargetPi, path: "settings.json", grammar: JSON, class: SharedConfiguration, slots: extensionMembership},
	{target: target.TargetPi, scope: target.ScopeGlobal, path: "models.json", grammar: JSONLineComments, class: ModelCatalog},
	{target: target.TargetPi, path: "mcp.json", grammar: JSON, class: SharedConfiguration},
	{target: target.TargetCodex, path: "config.toml", grammar: TOML, class: SharedConfiguration, slots: mcpEntries | hookSets | extensionMembership},
	{target: target.TargetCodex, scope: target.ScopeGlobal, namedProfile: true, grammar: TOML, class: SharedConfiguration, slots: mcpEntries | hookSets | extensionMembership},
	{target: target.TargetClaudeCode, path: "settings.json", grammar: JSON, class: SharedConfiguration, slots: hookSets | extensionMembership},
	{target: target.TargetClaudeCode, scope: target.ScopeProject, path: "settings.local.json", grammar: JSON, class: SharedConfiguration, slots: hookSets | extensionMembership},
	{target: target.TargetClaudeCode, scope: target.ScopeGlobal, path: ".claude.json", dedicatedOnly: true, grammar: JSON, class: SharedConfiguration},
	{target: target.TargetClaudeCode, scope: target.ScopeProject, path: ".mcp.json", dedicatedOnly: true, grammar: JSON, class: SharedConfiguration},
	{target: target.TargetOpenCode, path: "opencode.json", grammar: JSONC, class: SharedConfiguration, alternate: "opencode.jsonc", observations: openCodeCandidates, slots: mcpEntries | extensionMembership},
	{target: target.TargetOpenCode, path: "opencode.jsonc", grammar: JSONC, class: SharedConfiguration, alternate: "opencode.json", observations: openCodeCandidates, slots: mcpEntries | extensionMembership},
	{target: target.TargetOpenCode, path: "tui.json", grammar: JSONC, class: SharedConfiguration, alternate: "tui.jsonc", observations: openCodeCandidates, slots: extensionMembership},
	{target: target.TargetOpenCode, path: "tui.jsonc", grammar: JSONC, class: SharedConfiguration, alternate: "tui.json", observations: openCodeCandidates, slots: extensionMembership},
	{target: target.TargetOpenCode, scope: target.ScopeProject, path: ".opencode/opencode.json", grammar: JSONC, class: SharedConfiguration, alternate: ".opencode/opencode.jsonc", observations: openCodeCandidates, slots: mcpEntries | extensionMembership},
	{target: target.TargetOpenCode, scope: target.ScopeProject, path: ".opencode/opencode.jsonc", grammar: JSONC, class: SharedConfiguration, alternate: ".opencode/opencode.json", observations: openCodeCandidates, slots: mcpEntries | extensionMembership},
	{target: target.TargetOpenCode, scope: target.ScopeProject, path: ".opencode/tui.json", grammar: JSONC, class: SharedConfiguration, alternate: ".opencode/tui.jsonc", observations: openCodeCandidates, slots: extensionMembership},
	{target: target.TargetOpenCode, scope: target.ScopeProject, path: ".opencode/tui.jsonc", grammar: JSONC, class: SharedConfiguration, alternate: ".opencode/tui.json", observations: openCodeCandidates, slots: extensionMembership},
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

type nativePathReservation struct {
	path       string
	owner      string
	globalOnly bool
}

type nativePathPolicy struct {
	paths           []nativePathReservation
	moduleDirectory string
	runtimeFiles    bool
}

var nativePathPolicies = map[target.Target]nativePathPolicy{
	target.TargetPi: {
		moduleDirectory: "extensions",
		paths: []nativePathReservation{
			{"auth.json", "authentication", true},
			{"auth.json.lock", "authentication locking", true},
			{"settings.json.lock", "host settings locking", false},
			{"models-store.json", "the refreshed model cache", true},
			{"models-store.json.lock", "model cache locking", true},
			{"sessions", "session state", true},
			{"crashes.json", "crash diagnostics", true},
			{"pi-debug.log", "runtime diagnostics", true},
			{"npm", "package installation", false},
			{"git", "package installation", false},
			{"tmp", "temporary runtime artifacts", true},
			{"bin", "installed helper code", true},
			{"tools", "host tool artifacts", true},
		},
	},
	target.TargetCodex: {
		runtimeFiles: true,
		paths: []nativePathReservation{
			{"auth.json", "authentication", true},
			{".credentials.json", "MCP authentication", true},
			{"secrets", "encrypted credentials", true},
			{"mcp-oauth-locks", "MCP authentication locking", true},
			{"sessions", "session state", true},
			{"archived_sessions", "session state", true},
			{"history.jsonl", "message history", true},
			{"session_index.jsonl", "the session index", true},
			{"session_index.jsonl.tmp", "session index publication", true},
			{"shell_snapshots", "runtime shell snapshots", true},
			{"memories", "generated memory state", true},
			{"installation_id", "runtime identity", true},
			{"db-backups", "database recovery", true},
			{"log", "runtime logs", true},
			{"models_cache.json", "the model cache", true},
			{"version.json", "update state", true},
			{"cloud-config-bundle-cache.json", "the cloud configuration cache", true},
			{"cache/remote_plugin_catalog", "the plugin catalog cache", true},
			{"cache/codex_app_directory", "the connector directory cache", true},
			{"plugins/cache", "installed plugin code", true},
			{".tmp/marketplaces", "installed marketplaces", true},
			{".tmp/plugin-share-local-paths-v1.json", "plugin sharing state", true},
			{".tmp/rollout-compression.lock", "session compression locking", true},
			{"tmp/arg0", "runtime helper code", true},
			{"packages/standalone", "the standalone installation", true},
			{"app-server-control", "app-server control", true},
			{"app-server-daemon/daemon.lock", "daemon locking", true},
			{".sandbox", "sandbox state", true},
			{".sandbox-bin", "sandbox helper code", true},
			{".sandbox-secrets", "sandbox credentials", true},
		},
	},
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
