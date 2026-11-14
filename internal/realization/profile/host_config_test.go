package profile

import (
	"errors"
	"slices"
	"testing"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func hostConfigInput(selected target.Target, scope target.Scope, nativePath string) configuration.PlacementInput {
	return configuration.PlacementInput{Target: selected, Scope: scope, Path: nativePath}
}

func mustHostConfigPlacement(t *testing.T, input configuration.PlacementInput) configuration.Placement {
	t.Helper()
	placement, err := ResolveHostConfigPlacement(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := placement.Validate(); err != nil {
		t.Fatal(err)
	}
	return placement
}

func mustHostConfigKey(t *testing.T, segments ...string) output.Footprint {
	t.Helper()
	key, err := output.NewKeyPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	return footprint
}

func TestHostConfigStaticPlacementFacts(t *testing.T) {
	for _, tc := range []struct {
		target target.Target
		scope  target.Scope
		root   string
	}{
		{target.TargetPi, target.ScopeGlobal, "@pi-agent/"},
		{target.TargetPi, target.ScopeProject, ".pi/"},
		{target.TargetCodex, target.ScopeGlobal, "@codex-home/"},
		{target.TargetCodex, target.ScopeProject, ".codex/"},
	} {
		t.Run(tc.root, func(t *testing.T) {
			input := hostConfigInput(tc.target, tc.scope, "extensions/subagent/config.json")
			placement := mustHostConfigPlacement(t, input)
			if placement.Target() != tc.target || placement.Scope() != tc.scope ||
				placement.NativePath() != input.Path || placement.Destination().String() != tc.root+input.Path ||
				placement.Ownership() != desiredhostconfig.OwnershipKeys || placement.Grammar() != configuration.JSON ||
				placement.RequestedSyntax() != desiredhostconfig.SyntaxDefault {
				t.Fatalf("incorrect placement facts: %#v", placement)
			}
			if err := ValidateHostConfigFootprint(placement, mustHostConfigKey(t, "globalConcurrencyLimit")); err != nil {
				t.Fatal(err)
			}
			input.Path = "another-config.json"
			if other := mustHostConfigPlacement(t, input); other.ID() != placement.ID() {
				t.Fatal("static placement identity depends on an individual document")
			}
			want := []configuration.Observation{configuration.ReferentAliases, configuration.InstalledClaims}
			if tc.target == target.TargetPi {
				want = append(want, configuration.PiLoader)
			} else {
				want = append(want, configuration.CodexRuntimeLocations)
			}
			observations := placement.RequiredObservations()
			if !slices.Equal(observations, want) {
				t.Fatalf("observations = %v, want %v", observations, want)
			}
			observations[0] = "changed"
			if !slices.Equal(placement.RequiredObservations(), want) {
				t.Fatal("caller changed placement observation requirements")
			}
		})
	}
}

func TestHostConfigReservedGlobalPathsInBothModes(t *testing.T) {
	common := []string{"AGENTS.md", "skills/demo/SKILL.md", "auth.json", ".git/config"}
	paths := map[target.Target][]string{
		target.TargetPi: {
			"mcp.json", "sessions/one.jsonl", "npm/package.json", "git/example/demo/config.json",
			"tmp/extensions/item.json", "bin/tool", "tools/tool", "models-store.json", "crashes.json", "pi-debug.log",
			"auth.json.lock", "settings.json.lock", "models-store.json.lock", "models.json/child.json",
			"extensions", "extensions/demo.ts", "extensions/demo.js", "extensions/index.ts",
			"extensions/package.json", "extensions/.ignore", "extensions/.gitignore", "extensions/.fdignore",
			"extensions/demo/index.ts", "extensions/demo/index.js", "extensions/demo/package.json",
		},
		target.TargetCodex: {
			"AGENTS.override.md", "hooks.json", ".credentials.json", "secrets/local.age", "mcp-oauth-locks/file-store.lock",
			"sessions/one.jsonl", "archived_sessions/one.jsonl.zst", "history.jsonl", "session_index.jsonl", "session_index.jsonl.tmp",
			"shell_snapshots/thread.sh", "memories/raw_memories.md", "installation_id", "db-backups/state.sqlite",
			"log/codex-tui.log", "models_cache.json", "version.json", "cloud-config-bundle-cache.json",
			"cache/remote_plugin_catalog/item.json", "cache/codex_app_directory/item.json",
			"plugins/cache/market/demo/plugin.json", ".tmp/marketplaces/demo/file",
			".tmp/plugin-share-local-paths-v1.json", ".tmp/rollout-compression.lock", "tmp/arg0/helper",
			"packages/standalone/current", "app-server-control/startup.lock", "app-server-daemon/daemon.lock",
			"app-server-daemon/app-server.pid", "app-server-daemon/app-server-updater.pid.tmp",
			"app-server-daemon/app-server.pid.lock", "app-server-daemon/app-server.stderr.log",
			".sandbox/state.json", ".sandbox-bin/helper.exe", ".sandbox-secrets/users.json",
			"state_5.sqlite", "logs_2.sqlite-wal", "goals_1.sqlite-shm", "memories_1.sqlite", "thread_history_1.sqlite",
			"state_3.sqlite", "state_5.sqlite/child.json", "config.toml/child.json",
		},
	}
	for selected, nativePaths := range paths {
		for _, scope := range []target.Scope{target.ScopeGlobal} {
			for _, ownership := range []desiredhostconfig.Ownership{desiredhostconfig.OwnershipKeys, desiredhostconfig.OwnershipDocument} {
				for _, nativePath := range append(slices.Clone(common), nativePaths...) {
					input := hostConfigInput(selected, scope, nativePath)
					input.Ownership, input.Syntax = ownership, desiredhostconfig.SyntaxJSON
					_, err := ResolveHostConfigPlacement(input)
					if !errors.Is(err, configuration.ErrReserved) {
						t.Fatalf("%s/%s/%s %q = %v, want reserved", selected, scope, ownership, nativePath, err)
					}
				}
			}
		}
	}
}

func TestHostConfigOrdinaryDefinitionsRemainAvailable(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			for _, nativePath := range []string{
				"agents/reviewer.md", "prompts/review.md", "themes/theme.json", "plugins/data/demo/settings.json", "SYSTEM.md", "APPEND_SYSTEM.md",
				"extensions/subagent/config.json", "extensions/demo/nested/config.toml", "extensions/demo/nested/script.ts",
				"skills_extra/config.json", "auth.json.extra", "custom.sqlite", "custom.jsonl", "custom.md",
				"state_custom.sqlite", "my_state_5.sqlite", "state_5.sqlite-shm-wal",
				"environments.toml", "app-server-daemon/settings.json", "rules/default.rules", "profiles/review.config.toml",
			} {
				input := hostConfigInput(selected, scope, nativePath)
				input.Ownership = desiredhostconfig.OwnershipDocument
				placement := mustHostConfigPlacement(t, input)
				if err := ValidateHostConfigFootprint(placement, output.WholePathFootprint()); err != nil {
					t.Fatalf("%s/%s %q: %v", selected, scope, nativePath, err)
				}
			}
		}
	}
}

func TestHostConfigSharedDocumentsAndNativeReservations(t *testing.T) {
	for _, tc := range []struct {
		target target.Target
		path   string
		fields []string
	}{
		{target.TargetPi, "settings.json", []string{"packages", "extensions", "skills"}},
		{target.TargetCodex, "config.toml", []string{"mcp_servers", "hooks", "plugins", "marketplaces"}},
		{target.TargetCodex, "review.config.toml", []string{"mcp_servers", "hooks", "plugins", "marketplaces"}},
	} {
		for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			if tc.path == "review.config.toml" && scope == target.ScopeProject {
				continue
			}
			input := hostConfigInput(tc.target, scope, tc.path)
			placement := mustHostConfigPlacement(t, input)
			for _, field := range tc.fields {
				for _, segments := range [][]string{{field}, {field, "entry"}} {
					if err := ValidateHostConfigFootprint(placement, mustHostConfigKey(t, segments...)); !errors.Is(err, configuration.ErrReserved) {
						t.Fatalf("%s %q: %v, want reserved", tc.path, segments, err)
					}
				}
				for _, literal := range []string{field + "_extra", field + "/entry", field + "~entry"} {
					if err := ValidateHostConfigFootprint(placement, mustHostConfigKey(t, literal)); err != nil {
						t.Fatalf("literal native key %q: %v", literal, err)
					}
				}
			}
			for _, segments := range [][]string{{""}, {"."}, {"0"}, {"tui", "theme"}, {"subagents", "agentOverrides"}, {"enabledModels"}} {
				if err := ValidateHostConfigFootprint(placement, mustHostConfigKey(t, segments...)); err != nil {
					t.Fatal(err)
				}
			}
			input.Ownership = desiredhostconfig.OwnershipDocument
			if _, err := ResolveHostConfigPlacement(input); !errors.Is(err, configuration.ErrReserved) {
				t.Fatalf("whole shared document: %v", err)
			}
		}
	}
	pi := mustHostConfigPlacement(t, hostConfigInput(target.TargetPi, target.ScopeGlobal, "settings.json"))
	for _, field := range []string{"prompts", "themes", "npmCommand"} {
		if err := ValidateHostConfigFootprint(pi, mustHostConfigKey(t, field)); err != nil {
			t.Fatalf("host preference %q was given a new dedicated owner: %v", field, err)
		}
	}
}

func TestHostConfigGrammarSelection(t *testing.T) {
	for _, tc := range []struct {
		target    target.Target
		scope     target.Scope
		path      string
		ownership desiredhostconfig.Ownership
		syntax    desiredhostconfig.Syntax
		want      configuration.Grammar
	}{
		{target.TargetPi, target.ScopeGlobal, "settings.json", "", "", configuration.JSON},
		{target.TargetPi, target.ScopeGlobal, "models.json", "", "", configuration.JSONLineComments},
		{target.TargetPi, target.ScopeProject, "models.json", "", "", configuration.JSON},
		{target.TargetCodex, target.ScopeGlobal, "config.toml", "", "", configuration.TOML},
		{target.TargetCodex, target.ScopeGlobal, "review.config.toml", "", "", configuration.TOML},
		{target.TargetPi, target.ScopeGlobal, "custom.JSON", "", "", configuration.JSON},
		{target.TargetPi, target.ScopeGlobal, "custom.JsonC", "", "", configuration.JSONC},
		{target.TargetPi, target.ScopeGlobal, "custom.TOML", "", "", configuration.TOML},
		{target.TargetPi, target.ScopeGlobal, "custom", "", desiredhostconfig.SyntaxTOML, configuration.TOML},
		{target.TargetPi, target.ScopeGlobal, "custom.json", "", desiredhostconfig.SyntaxJSONLineComments, configuration.JSONLineComments},
		{target.TargetPi, target.ScopeGlobal, "reviewer.md", desiredhostconfig.OwnershipDocument, "", configuration.Opaque},
		{target.TargetPi, target.ScopeGlobal, "config.yaml", desiredhostconfig.OwnershipDocument, "", configuration.Opaque},
		{target.TargetCodex, target.ScopeProject, "review.config.toml", desiredhostconfig.OwnershipDocument, "", configuration.TOML},
	} {
		input := hostConfigInput(tc.target, tc.scope, tc.path)
		input.Ownership, input.Syntax = tc.ownership, tc.syntax
		placement := mustHostConfigPlacement(t, input)
		if placement.Grammar() != tc.want || placement.RequestedSyntax() != tc.syntax {
			t.Fatalf("%s/%s %q grammar=%q requested=%q", tc.target, tc.scope, tc.path, placement.Grammar(), placement.RequestedSyntax())
		}
	}
	for _, tc := range []struct {
		path   string
		syntax desiredhostconfig.Syntax
	}{
		{"settings.json", desiredhostconfig.SyntaxJSONC},
		{"models.json", desiredhostconfig.SyntaxJSON},
		{"models.json", desiredhostconfig.SyntaxJSONC},
		{"config.yaml", ""},
		{"reviewer.md", ""},
	} {
		input := hostConfigInput(target.TargetPi, target.ScopeGlobal, tc.path)
		input.Syntax = tc.syntax
		if _, err := ResolveHostConfigPlacement(input); !errors.Is(err, configuration.ErrGrammar) {
			t.Fatalf("%s/%s = %v, want grammar refusal", tc.path, tc.syntax, err)
		}
	}
}

func TestHostConfigStaticValidationDoesNotCertifyValuesOrEffects(t *testing.T) {
	placement := mustHostConfigPlacement(t, hostConfigInput(target.TargetPi, target.ScopeGlobal, "models.json"))
	for _, key := range [][]string{{"providers", "example", "apiKey"}, {"providers", "example", "headers"}, {"a", "0"}} {
		if err := ValidateHostConfigFootprint(placement, mustHostConfigKey(t, key...)); err != nil {
			t.Fatalf("static validation interpreted value/schema at %q: %v", key, err)
		}
	}
	if err := ValidateHostConfigFootprint(placement, output.WholePathFootprint()); err == nil {
		t.Fatal("key placement accepted whole-path authority")
	}
	input := hostConfigInput(target.TargetPi, target.ScopeGlobal, "agents/reviewer.md")
	input.Ownership = desiredhostconfig.OwnershipDocument
	document := mustHostConfigPlacement(t, input)
	if err := ValidateHostConfigFootprint(document, mustHostConfigKey(t, "a")); err == nil {
		t.Fatal("whole-document placement accepted partial authority")
	}
	if err := ValidateHostConfigFootprint(configuration.Placement{}, mustHostConfigKey(t, "a")); err == nil {
		t.Fatal("zero placement accepted authority")
	}
}
