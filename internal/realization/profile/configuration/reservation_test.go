package configuration_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestProtectionRequirementsDoNotInferKnownNoneFromMissingFacts(t *testing.T) {
	if _, err := (configuration.ProtectionRequirements{}).References(); err == nil {
		t.Fatal("unknown requirements became known-no-protection")
	}
	for _, roots := range [][]output.RootReference{nil, {}, {{}}} {
		if _, err := configuration.RequireProtectionRoots(roots); err == nil {
			t.Fatal("missing or invalid required roots became known-no-protection")
		}
	}
	references, err := configuration.NoProtectionRequirements().References()
	if err != nil || len(references) != 0 {
		t.Fatalf("explicit known-no-protection = %v, %v", references, err)
	}
}

func TestProtectionRequirementsRetainIndependentRootsWithoutBorrowingInput(t *testing.T) {
	data, err := output.NewRootReference("opencode-data", "fixture-data-v1")
	if err != nil {
		t.Fatal(err)
	}
	state, err := output.NewRootReference("opencode-state", "fixture-state-v1")
	if err != nil {
		t.Fatal(err)
	}
	input := []output.RootReference{state, data, state}
	requirements, err := configuration.RequireProtectionRoots(input)
	if err != nil {
		t.Fatal(err)
	}
	clear(input)
	references, err := requirements.References()
	if err != nil || len(references) != 2 || !slices.Contains(references, data) || !slices.Contains(references, state) {
		t.Fatalf("independent root requirements = %v, %v", references, err)
	}
	clear(references)
	references, err = requirements.References()
	if err != nil || len(references) != 2 || !slices.Contains(references, data) || !slices.Contains(references, state) {
		t.Fatalf("mutating returned references changed requirements = %v, %v", references, err)
	}
}

func TestExclusionRequirementsComeFromAuditedFacetFacts(t *testing.T) {
	policies, err := configuration.NativeExclusionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		facets, err := policy.Facets()
		if policy.Coverage() == configuration.ExclusionUnknown {
			if err == nil || facets != nil {
				t.Fatal("unaudited exclusion facets supplied partial protection facts")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		needsObservation := false
		for _, facet := range facets {
			if err := facet.Reference().Validate(); err != nil {
				t.Fatal("exclusion facet lost its locator contract", err)
			}
			needsObservation = needsObservation || facet.RequiresObservation()
		}
		if needsObservation == (policy.Coverage() == configuration.ExclusionKnownNone) {
			t.Fatal("audited coverage disagrees with its complete facet facts")
		}
	}
}

func TestProtectionRequirementsPreserveDistinctLocatorContractsForOneIdentity(t *testing.T) {
	first, err := output.NewRootReference("fixture-root", "fixture-root-v1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := output.NewRootReference("fixture-root", "fixture-root-v2")
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := configuration.RequireProtectionRoots([]output.RootReference{first, second, first})
	if err != nil {
		t.Fatal(err)
	}
	references, err := requirements.References()
	if err != nil || len(references) != 2 || !slices.Contains(references, first) || !slices.Contains(references, second) {
		t.Fatalf("distinct locator contracts = %v, %v", references, err)
	}
}

func reservationKey(t *testing.T, segments ...string) output.KeyPath {
	t.Helper()
	key, err := output.NewKeyPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func reservationAlias(t *testing.T, value string) output.Destination {
	t.Helper()
	alias, err := output.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return alias
}

func TestDedicatedPathsProjectOwnerAliasesWithoutTakingSharedDocuments(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		scope    target.Scope
		mcp      []string
		hooks    []string
		resource []string
		reserved []string
		free     []string
	}{
		{
			target.TargetPi, target.ScopeGlobal,
			[]string{"~/.pi/agent/mcp.json"},
			nil,
			[]string{"~/.pi/agent/AGENTS.md", "@pi-agent/skills", "~/.agents/skills"},
			[]string{"mcp.json", "mcp.json/child", "AGENTS.md", "skills", "skills/demo"},
			[]string{"settings.json", "skills_extra", "agents/skills"},
		},
		{
			target.TargetPi, target.ScopeProject,
			[]string{".pi/mcp.json"},
			nil,
			[]string{"AGENTS.md", ".pi/skills"},
			[]string{"mcp.json", "skills/demo"},
			[]string{"AGENTS.md", "settings.json"},
		},
		{
			target.TargetCodex, target.ScopeGlobal,
			[]string{"~/.codex/config.toml"},
			[]string{"~/.codex/hooks.json"},
			[]string{"~/.codex/AGENTS.override.md", "~/.codex/skills"},
			[]string{"hooks.json", "hooks.json/child", "AGENTS.override.md", "skills/demo"},
			[]string{"config.toml", "review.config.toml", "skills_extra"},
		},
		{
			target.TargetCodex, target.ScopeProject,
			[]string{".codex/config.toml"},
			[]string{".codex/hooks.json"},
			[]string{"AGENTS.md", ".agents/skills"},
			[]string{"hooks.json"},
			[]string{"config.toml", "AGENTS.md", "skills/demo"},
		},
		{
			target.TargetClaudeCode, target.ScopeGlobal,
			[]string{"~/.claude.json"},
			[]string{"~/.claude/settings.json"},
			[]string{"~/.claude/CLAUDE.md", "~/.claude/skills"},
			[]string{"CLAUDE.md", "skills/demo"},
			[]string{"settings.json", ".claude.json"},
		},
		{
			target.TargetOpenCode, target.ScopeProject,
			[]string{".opencode/opencode.json"},
			nil,
			[]string{".opencode/skills", "AGENTS.md"},
			[]string{".opencode/skills/demo"},
			[]string{"opencode.json", ".opencode/opencode.json", ".opencode/tui.json"},
		},
		{
			target.TargetOpenCode, target.ScopeGlobal,
			[]string{"~/.config/opencode/opencode.json"},
			nil,
			[]string{"~/.config/opencode/AGENTS.md"},
			[]string{"AGENTS.md"},
			[]string{"opencode.json", "tui.json"},
		},
		{
			target.TargetAntigravityCLI, target.ScopeGlobal,
			[]string{"~/.gemini/config/mcp_config.json"},
			nil,
			[]string{"~/.gemini/config/skills"},
			nil,
			[]string{"settings.json", "keybindings.json", "skills/demo"},
		},
	} {
		t.Run(string(test.selected)+"/"+string(test.scope), func(t *testing.T) {
			input := configuration.DedicatedReservationsInput{Target: test.selected, Scope: test.scope}
			for _, alias := range test.mcp {
				input.MCPDocuments = append(input.MCPDocuments, reservationAlias(t, alias))
			}
			for _, alias := range test.hooks {
				input.HookDocuments = append(input.HookDocuments, reservationAlias(t, alias))
			}
			for _, alias := range test.resource {
				input.Resources = append(input.Resources, reservationAlias(t, alias))
			}
			facts, err := configuration.NewDedicatedReservations(input)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range test.reserved {
				if err := facts.RequireUnreservedPath(path); !errors.Is(err, configuration.ErrReservedPath) {
					t.Fatalf("%q lost its dedicated owner: %v", path, err)
				}
			}
			for _, path := range test.free {
				if err := facts.RequireUnreservedPath(path); err != nil {
					t.Fatalf("%q acquired an unrelated reservation: %v", path, err)
				}
			}
		})
	}
}

func TestReservationAliasesDoNotBroadenDocumentAdmission(t *testing.T) {
	alias := reservationAlias(t, "~/.pi/agent/skills")
	resources := []output.Destination{alias}
	facts, err := configuration.NewDedicatedReservations(configuration.DedicatedReservationsInput{
		Target: target.TargetPi, Scope: target.ScopeGlobal, Resources: resources,
	})
	if err != nil {
		t.Fatal(err)
	}
	resources[0] = reservationAlias(t, "@pi-agent/other")
	if !errors.Is(facts.RequireUnreservedPath("skills/demo"), configuration.ErrReservedPath) {
		t.Fatal("legacy home alias or caller mutation lost the reserved subtree")
	}
	if _, err := configuration.DocumentSpecForAlias(configuration.AliasInput{
		Target: target.TargetPi, Scope: target.ScopeGlobal, Destination: alias,
	}); !errors.Is(err, configuration.ErrDocumentPath) {
		t.Fatalf("reservation interpretation admitted a generic home alias: %v", err)
	}
	if (configuration.DedicatedReservations{}).RequireUnreservedPath("settings.json") == nil {
		t.Fatal("zero coverage admitted a path")
	}
	if _, err := configuration.NewDedicatedReservations(configuration.DedicatedReservationsInput{
		Target: target.TargetPi, Scope: target.ScopeGlobal, Resources: []output.Destination{{}},
	}); err == nil {
		t.Fatal("unobserved resource location became empty coverage")
	}
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
			facts, err := configuration.NewDedicatedReservations(configuration.DedicatedReservationsInput{
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
	input := configuration.DedicatedReservationsInput{Target: target.TargetOpenCode, Scope: target.ScopeProject}
	missing, err := configuration.NewDedicatedReservations(input)
	if err != nil {
		t.Fatal(err)
	}
	if missing.ValidateDocument(spec) == nil || (configuration.DedicatedReservations{}).ValidateDocument(spec) == nil {
		t.Fatal("missing required coverage became known-empty")
	}
	input.Extensions = []output.KeyPath{reservationKey(t, "plugin")}
	facts, err := configuration.NewDedicatedReservations(input)
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
	if (configuration.DedicatedReservations{}).RequireUnreservedKey(configuration.DocumentSpec{}, reservationKey(t, "unrelated")) == nil {
		t.Fatal("zero document and coverage bypassed the key guard")
	}
	wrongScope, err := configuration.NewDedicatedReservations(configuration.DedicatedReservationsInput{
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
	if _, err := configuration.NewDedicatedReservations(input); err == nil {
		t.Fatal("root coverage was admitted")
	}
	ag, err := configuration.NewDocumentSpec(configuration.DocumentInput{Target: target.TargetAntigravityCLI, Scope: target.ScopeGlobal, Path: "settings.json"})
	if err != nil {
		t.Fatal(err)
	}
	if facts.ValidateDocument(ag) == nil {
		t.Fatal("another target/scope's coverage was admitted")
	}
	unselected, err := configuration.NewDedicatedReservations(configuration.DedicatedReservationsInput{Target: target.TargetAntigravityCLI, Scope: target.ScopeGlobal})
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
			input := configuration.DedicatedReservationsInput{
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
			facts, err := configuration.NewDedicatedReservations(input)
			if err != nil {
				t.Fatal(err)
			}
			if facts.RequireUnreservedKey(spec, reservationKey(t, "unrelated")) == nil {
				t.Fatal("an unrelated selection bypassed missing required coverage")
			}
		})
	}
}
