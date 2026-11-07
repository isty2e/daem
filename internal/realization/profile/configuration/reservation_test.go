package configuration_test

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func reservationKey(t *testing.T, segments ...string) output.KeyPath {
	t.Helper()
	key, err := output.NewKeyPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestDedicatedRolesFollowDocumentSemanticsRatherThanHostUnion(t *testing.T) {
	const (
		mcp = 1 << iota
		hooks
		extensions
	)
	for _, test := range []struct {
		selected target.Target
		scope    target.Scope
		path     string
		alias    string
		roles    uint8
	}{
		{target.TargetPi, target.ScopeGlobal, "settings.json", "", extensions},
		{target.TargetPi, target.ScopeProject, "settings.json", "", extensions},
		{target.TargetPi, target.ScopeGlobal, "models.json", "", 0},
		{target.TargetPi, target.ScopeGlobal, "mcp.json", "", 0},
		{target.TargetCodex, target.ScopeGlobal, "config.toml", "", mcp | hooks | extensions},
		{target.TargetCodex, target.ScopeProject, "config.toml", "", mcp | hooks | extensions},
		{target.TargetCodex, target.ScopeGlobal, "review.config.toml", "", mcp | hooks | extensions},
		{target.TargetClaudeCode, target.ScopeGlobal, "settings.json", "", hooks | extensions},
		{target.TargetClaudeCode, target.ScopeProject, "settings.local.json", "", hooks | extensions},
		{target.TargetClaudeCode, target.ScopeGlobal, "", "~/.claude.json", 0},
		{target.TargetClaudeCode, target.ScopeProject, "", ".mcp.json", 0},
		{target.TargetOpenCode, target.ScopeGlobal, "opencode.json", "", mcp | extensions},
		{target.TargetOpenCode, target.ScopeGlobal, "opencode.jsonc", "", mcp | extensions},
		{target.TargetOpenCode, target.ScopeProject, ".opencode/opencode.json", "", mcp | extensions},
		{target.TargetOpenCode, target.ScopeProject, ".opencode/opencode.jsonc", "", mcp | extensions},
		{target.TargetOpenCode, target.ScopeGlobal, "tui.json", "", extensions},
		{target.TargetOpenCode, target.ScopeGlobal, "tui.jsonc", "", extensions},
		{target.TargetOpenCode, target.ScopeProject, ".opencode/tui.json", "", extensions},
		{target.TargetOpenCode, target.ScopeProject, ".opencode/tui.jsonc", "", extensions},
		{target.TargetAntigravityCLI, target.ScopeGlobal, "settings.json", "", 0},
		{target.TargetAntigravityCLI, target.ScopeGlobal, "keybindings.json", "", 0},
		{target.TargetAntigravityCLI, target.ScopeGlobal, "", "~/.gemini/config/mcp_config.json", 0},
	} {
		t.Run(string(test.selected)+"/"+string(test.scope)+"/"+test.path+test.alias, func(t *testing.T) {
			var spec configuration.DocumentSpec
			var err error
			if test.alias == "" {
				spec, err = configuration.NewDocumentSpec(configuration.DocumentInput{Target: test.selected, Scope: test.scope, Path: test.path})
			} else {
				alias, parseErr := output.Parse(test.alias)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				spec, err = configuration.DocumentSpecForAlias(configuration.AliasInput{Target: test.selected, Scope: test.scope, Destination: alias})
			}
			if err != nil {
				t.Fatal(err)
			}
			facts, err := configuration.NewDedicatedKeys(configuration.DedicatedKeysInput{
				Target: test.selected, Scope: test.scope,
				MCP: []output.KeyPath{reservationKey(t, "mcp")}, Hooks: []output.KeyPath{reservationKey(t, "hooks")},
				Extensions: []output.KeyPath{reservationKey(t, "plugin")},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, probe := range []struct {
				role uint8
				key  string
			}{{mcp, "mcp"}, {hooks, "hooks"}, {extensions, "plugin"}} {
				err := facts.RequireUnreservedKey(spec, reservationKey(t, probe.key, "child"))
				if test.roles&probe.role != 0 {
					if !errors.Is(err, configuration.ErrReservedKey) {
						t.Fatalf("selected %s role did not reserve its subtree: %v", probe.key, err)
					}
				} else if err != nil {
					t.Fatalf("unselected %s role widened reservation: %v", probe.key, err)
				}
			}
		})
	}
}

func TestDedicatedCoverageDistinguishesMissingFromUnselectedAndCopiesInputs(t *testing.T) {
	spec, err := configuration.NewDocumentSpec(configuration.DocumentInput{Target: target.TargetOpenCode, Scope: target.ScopeProject, Path: "tui.json"})
	if err != nil {
		t.Fatal(err)
	}
	input := configuration.DedicatedKeysInput{Target: target.TargetOpenCode, Scope: target.ScopeProject}
	missing, err := configuration.NewDedicatedKeys(input)
	if err != nil {
		t.Fatal(err)
	}
	if missing.ValidateDocument(spec) == nil || (configuration.DedicatedKeys{}).ValidateDocument(spec) == nil {
		t.Fatal("missing required coverage became known-empty")
	}
	input.Extensions = []output.KeyPath{reservationKey(t, "plugin")}
	facts, err := configuration.NewDedicatedKeys(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Extensions[0] = reservationKey(t, "other")
	if !errors.Is(facts.RequireUnreservedKey(spec, reservationKey(t, "plugin", "child")), configuration.ErrReservedKey) {
		t.Fatal("caller mutation changed canonical coverage")
	}
	if err := facts.RequireUnreservedKey(spec, reservationKey(t, "mcp", "server")); err != nil {
		t.Fatalf("unselected MCP coverage was required by TUI: %v", err)
	}
	if facts.RequireUnreservedKey(spec, output.KeyPath{}) == nil {
		t.Fatal("root selector became key ownership")
	}
	if (configuration.DedicatedKeys{}).RequireUnreservedKey(configuration.DocumentSpec{}, reservationKey(t, "unrelated")) == nil {
		t.Fatal("zero document and coverage bypassed the key guard")
	}
	wrongScope, err := configuration.NewDedicatedKeys(configuration.DedicatedKeysInput{
		Target: target.TargetOpenCode, Scope: target.ScopeGlobal,
		Extensions: []output.KeyPath{reservationKey(t, "plugin")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if wrongScope.ValidateDocument(spec) == nil {
		t.Fatal("another scope's coverage was admitted")
	}
	input.Extensions = []output.KeyPath{{}}
	if _, err := configuration.NewDedicatedKeys(input); err == nil {
		t.Fatal("root coverage was admitted")
	}
	ag, err := configuration.NewDocumentSpec(configuration.DocumentInput{Target: target.TargetAntigravityCLI, Scope: target.ScopeGlobal, Path: "settings.json"})
	if err != nil {
		t.Fatal(err)
	}
	if facts.ValidateDocument(ag) == nil {
		t.Fatal("another target/scope's coverage was admitted")
	}
	unselected, err := configuration.NewDedicatedKeys(configuration.DedicatedKeysInput{Target: target.TargetAntigravityCLI, Scope: target.ScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	if err := unselected.ValidateDocument(ag); err != nil {
		t.Fatalf("unselected coverage became a dependency: %v", err)
	}
}

func TestEverySelectedDedicatedRoleRequiresCoverage(t *testing.T) {
	spec, err := configuration.NewDocumentSpec(configuration.DocumentInput{
		Target: target.TargetCodex, Scope: target.ScopeProject, Path: "config.toml",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, missingRole := range []string{"MCP", "hooks", "extensions"} {
		t.Run(missingRole, func(t *testing.T) {
			input := configuration.DedicatedKeysInput{
				Target: target.TargetCodex, Scope: target.ScopeProject,
				MCP: []output.KeyPath{reservationKey(t, "mcp")}, Hooks: []output.KeyPath{reservationKey(t, "hooks")},
				Extensions: []output.KeyPath{reservationKey(t, "plugin")},
			}
			switch missingRole {
			case "MCP":
				input.MCP = nil
			case "hooks":
				input.Hooks = nil
			case "extensions":
				input.Extensions = nil
			}
			facts, err := configuration.NewDedicatedKeys(input)
			if err != nil {
				t.Fatal(err)
			}
			if facts.RequireUnreservedKey(spec, reservationKey(t, "unrelated")) == nil {
				t.Fatal("an unrelated selection bypassed missing required coverage")
			}
		})
	}
}
