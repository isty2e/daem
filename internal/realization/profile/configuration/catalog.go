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

func (fact rootFact) reference() (output.RootReference, error) {
	return output.NewRootReference(fact.identity, fact.locator)
}

func ProjectRootReference() (output.RootReference, error) {
	return projectRoot.reference()
}

// RootReferenceForScope selects the default native-layout root for a host
// supplier. Document aliases may select a different dedicated root.
func RootReferenceForScope(selected target.Target, scope target.Scope) (output.RootReference, error) {
	layout, err := selectLayout(selected, scope)
	if err != nil {
		return output.RootReference{}, err
	}
	return layout.root.reference()
}

type nativeLayout struct {
	authoring      bool
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
	claudeCredentials = rootFact{"claude-credentials", "claude-credentials-location-v1"}
	claudePlugins     = rootFact{"claude-plugin-store", "claude-plugin-store-location-v1"}
	openCodeRoot      = rootFact{"opencode-configuration", "opencode-location-v1"}
	openCodeExtra     = rootFact{"opencode-additional-configuration", "opencode-additional-location-v1"}
	openCodeSources   = rootFact{"opencode-additional-sources", "opencode-source-location-v1"}
	openCodeInstall   = rootFact{"opencode-package-installation", "opencode-installation-location-v1"}
	openCodeLegacy    = rootFact{"opencode-legacy-configuration", "opencode-legacy-location-v1"}
	openCodeData      = rootFact{"opencode-data", "opencode-data-location-v1"}
	openCodeState     = rootFact{"opencode-state", "opencode-state-location-v1"}
	openCodeCache     = rootFact{"opencode-cache", "opencode-cache-location-v1"}
	openCodeDatabase  = rootFact{"opencode-database-override", "opencode-database-location-v1"}
	openCodeMetadata  = rootFact{"opencode-plugin-metadata-override", "opencode-plugin-metadata-location-v1"}
	antigravityRoot   = rootFact{"antigravity-preferences", "antigravity-preferences-location-v1"}
	antigravityShared = rootFact{"antigravity-shared", "antigravity-shared-location-v1"}
	antigravityState  = rootFact{"antigravity-state", "antigravity-state-location-v1"}
)

var nativeLayouts = map[scopeKey]nativeLayout{
	{target.TargetPi, target.ScopeGlobal}: {
		authoring: true,
		root:      piRoot, aliasRoot: output.RootPiAgent, legacyHomeDir: ".pi/agent/", policy: "pi-native-v1", observations: piLoader,
	},
	{target.TargetPi, target.ScopeProject}: {
		authoring: true,
		root:      projectRoot, aliasRoot: output.RootProject, aliasDirectory: ".pi/", relativePrefix: ".pi/", policy: "pi-native-v1", observations: piLoader,
	},
	{target.TargetCodex, target.ScopeGlobal}: {
		authoring: true,
		root:      codexRoot, aliasRoot: output.RootCodexHome, legacyHomeDir: ".codex/", policy: "codex-native-v1", observations: codexRuntime,
	},
	{target.TargetCodex, target.ScopeProject}: {
		authoring: true,
		root:      projectRoot, aliasRoot: output.RootProject, aliasDirectory: ".codex/", relativePrefix: ".codex/", policy: "codex-native-v1", observations: codexRuntime,
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

func (layout nativeLayout) bindingRelativePathFor(destination output.Destination) (string, bool) {
	if nativePath, matched := layout.nativePathFor(destination); matched {
		return layout.relativePrefix + nativePath, true
	}
	return destination.RelativePath(), destination.RootRole() == output.RootProject && layout.aliasRoot == output.RootProject
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

func dedicatedAliasFor(input AliasInput) (dedicatedAliasFact, bool) {
	for _, dedicated := range dedicatedDocumentAliases {
		if dedicated.target == input.Target && dedicated.scope == input.Scope && dedicated.alias == input.Destination.String() {
			return dedicated, true
		}
	}
	return dedicatedAliasFact{}, false
}

type documentRule struct {
	target        target.Target
	scope         target.Scope
	path          string
	namedProfile  bool
	dedicatedOnly bool
	primaryOnly   bool
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
	{target: target.TargetOpenCode, scope: target.ScopeGlobal, path: "config.json", primaryOnly: true, grammar: JSONC, class: SharedConfiguration, slots: mcpEntries | extensionMembership},
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

type nativePathFacet struct {
	root            rootFact
	prefix          string
	paths           []nativePathReservation
	moduleDirectory string
	runtimeFiles    bool
	directory       *nativeDirectoryReservation
	linkedEntries   []string
	fileCompanions  []string
	filePatterns    []nativeFilenameReservation
}

type nativeDirectoryReservation struct {
	owner      string
	exemptions []string
}

type nativeFilenameReservation struct {
	directory  string
	patterns   []string
	owner      string
	companions []string
}

type nativePathFacets []nativePathFacet

var nativePathPolicies = map[target.Target]nativePathFacets{
	target.TargetPi: {{
		root:            piRoot,
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
	}},
	target.TargetCodex: {{
		root:         codexRoot,
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
	}},
	target.TargetOpenCode: {
		npmConfigurationFacet(openCodeRoot),
		npmConfigurationFacet(openCodeLegacy),
		scriptConfigurationFacet(openCodeRoot),
		scriptConfigurationFacet(openCodeSources),
		scriptConfigurationFacet(openCodeLegacy),
		{
			root: openCodeData,
			directory: &nativeDirectoryReservation{
				owner: "authentication and runtime data", exemptions: []string{"worktree"},
			},
			filePatterns: []nativeFilenameReservation{
				{directory: ".", patterns: []string{"opencode.db", "opencode-*.db"}, owner: "authentication database", companions: sqliteCompanions},
			},
		},
		{root: openCodeState, directory: &nativeDirectoryReservation{owner: "runtime state"}},
		{
			root: openCodeCache, directory: &nativeDirectoryReservation{owner: "installed code and cache"},
			linkedEntries: []string{"bin", "packages", "packages/@*", "skills"},
		},
		npmConfigurationFacet(openCodeInstall),
		{
			root: openCodeDatabase, paths: []nativePathReservation{{".", "authentication database", true}},
			fileCompanions: sqliteCompanions,
		},
		{root: openCodeMetadata, paths: []nativePathReservation{{".", "plugin registration state", true}}},
	},
	target.TargetClaudeCode: {
		{
			root: claudePreferences,
			paths: []nativePathReservation{
				{".credentials.json", "authentication", true},
				{".session_ingress_token", "session authentication", true},
				{"projects", "session and generated memory state", true},
				{"sessions", "session state", true},
				{"storage-v2", "application storage", true},
				{"state", "application state", true},
				{"tasks", "task state", true},
				{"teams", "agent team state", true},
				{"jobs", "background job state", true},
				{"daemon", "background runtime state", true},
				{"agent-memory", "generated agent memory", true},
				{"file-history", "file recovery snapshots", true},
				{"plans", "generated session plans", true},
				{"debug", "runtime diagnostics", true},
				{"paste-cache", "session input cache", true},
				{"image-cache", "session image cache", true},
				{"uploads", "session attachments", true},
				{"session-env", "session environment state", true},
				{"shell-snapshots", "runtime shell snapshots", true},
				{"backups", "account recovery copies", true},
				{"feedback-bundles", "diagnostic archives", true},
				{"feedback", "diagnostic drafts", true},
				{"usage-data", "generated usage reports", true},
				{"cache", "application cache", true},
				{"scratch", "session scratch state", true},
				{"mcp-discovery-cache", "MCP discovery cache", true},
				{"mcp-skill-archives", "generated MCP artifacts", true},
				{"ide", "IDE connection state", true},
				{"todos", "legacy task state", true},
				{"statsig", "legacy runtime cache", true},
				{"logs", "legacy runtime diagnostics", true},
				{"plugins", "installed plugin code and state", true},
				{"cowork_plugins", "installed plugin code and state", true},
				{"dev-mods", "generated runtime code", true},
				{"skills/.trash", "retired synced skill code", true},
				{"history.jsonl", "message history", true},
				{"antproto.json", "runtime identity", true},
				{"daemon.status.json", "daemon status", true},
				{"active-time.json", "runtime accounting", true},
				{".last-cleanup", "cleanup state", true},
				{"gh-pr-status-cache.json", "runtime cache", true},
				{"server-sessions.json", "session index", true},
				{".deep-link-register-failed", "runtime registration state", true},
				{".last-update-result.json", "update state", true},
				{"daemon.scheduled.status.json", "scheduled runtime state", true},
				{"computer-use.lock", "runtime locking", true},
				{"daemon-auth-cooldown", "authentication state", true},
				{"daemon-auth-status.json", "authentication state", true},
				{"daemon.lock", "daemon locking", true},
				{"daemon.log", "daemon diagnostics", true},
				{"hfi-auth.json", "authentication", true},
				{"mcp-needs-auth-cache.json", "MCP authentication state", true},
				{".npm-cache-cleanup", "installation cleanup state", true},
				{"policy-limits.json", "server policy cache", true},
				{"policy-limits.json.stamp.json", "server policy cache identity", true},
				{"remote-settings.json", "server settings cache", true},
				{"remote-settings-consent.json", "server settings state", true},
				{"remote-settings-helper-consent", "server settings state", true},
				{"server.lock", "server locking", true},
				{".session-log-cleanup", "session cleanup state", true},
				{"stats-cache.json", "runtime counters", true},
				{".update.lock", "update locking", true},
				{".version-cleanup", "installation cleanup state", true},
			},
			linkedEntries: []string{"plugins", "plugins/cache", "plugins/cache/*", "plugins/cache/*/*", "plugins/marketplaces", "cowork_plugins", "cowork_plugins/cache", "cowork_plugins/cache/*", "cowork_plugins/cache/*/*", "cowork_plugins/marketplaces"},
		},
		{root: claudeAccount, paths: []nativePathReservation{{".claude.json.lock", "account configuration locking", true}, {".claude.json.backup", "legacy account recovery", true}}},
		{root: claudeCredentials, paths: []nativePathReservation{{".credentials.json", "authentication", true}}},
		{root: claudePlugins, directory: &nativeDirectoryReservation{owner: "installed plugin code and state"}, linkedEntries: []string{"cache", "cache/*", "cache/*/*", "marketplaces"}},
	},
	target.TargetAntigravityCLI: {
		{
			root: antigravityRoot,
			paths: []nativePathReservation{
				{"log", "runtime diagnostics", true},
				{"cache", "runtime cache", true},
				{"brain", "conversation state", true},
				{"mcp", "generated MCP server state", true},
				{"secplugins/grants", "trusted command grants", true},
				{"builtin", "host-supplied code", true},
				{"plugins", "installed plugin code", true},
				{"import_manifest.json", "plugin installation state", true},
				{"history.jsonl", "message history", true},
				{"antigravity-oauth-token", "legacy authentication", true},
			},
			linkedEntries: []string{"plugins", "builtin/plugins", "builtin/skills"},
		},
		{
			root: antigravityShared,
			paths: []nativePathReservation{
				{"plugins", "installed plugin code", true},
				{"import_manifest.json", "plugin installation state", true},
			},
			linkedEntries: []string{"plugins"},
		},
		{
			root: antigravityState,
			paths: []nativePathReservation{
				{"jetski-standalone-oauth-token", "authentication", true},
				{"cache/antigravity-keyring-unavailable", "authentication fallback state", true},
				{"memory", "generated memory state", true},
				{"antigravity-ide/installation_id", "runtime installation identity", true},
			},
		},
	},
}

func npmConfigurationFacet(root rootFact) nativePathFacet {
	return nativePathFacet{
		root: root,
		paths: []nativePathReservation{
			{".gitignore", "package installation metadata", false},
			{"node_modules", "installed package code", false},
			{"package.json", "package installation metadata", false},
			{"package-lock.json", "package installation metadata", false},
		},
		linkedEntries: []string{"node_modules", "node_modules/@*", "node_modules/.bin"},
	}
}

func scriptConfigurationFacet(root rootFact) nativePathFacet {
	return nativePathFacet{
		root: root,
		filePatterns: []nativeFilenameReservation{
			{directory: "plugin", patterns: []string{"*.ts", "*.js"}, owner: "plugin code"},
			{directory: "plugins", patterns: []string{"*.ts", "*.js"}, owner: "plugin code"},
		},
	}
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
