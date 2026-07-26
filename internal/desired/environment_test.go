package desired

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestEnvironmentOwnsDefensiveCollectionsAndValidatesDefaults(t *testing.T) {
	defaults := testDefaults(t)
	skills := []skill.Skill{testSkill(t, "direct", "direct")}
	targets := []target.Target{target.TargetCodex}
	environment, err := New(Spec{Targets: targets, Defaults: defaults, Skills: skills})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	targets[0] = target.TargetPi
	skills[0] = skill.Skill{}
	if environment.Targets()[0] != target.TargetCodex || environment.Skills()[0].ID().Name() != "direct" {
		t.Fatal("environment retained aliased constructor storage")
	}
	got := environment.Skills()
	got[0] = skill.Skill{}
	if environment.Skills()[0].ID().Name() != "direct" {
		t.Fatal("Skills returned aliased storage")
	}
	if environment.Validate() != nil {
		t.Fatal("environment validation mismatch")
	}
}

func TestEnvironmentOwnsEntityArtifactSourceBasis(t *testing.T) {
	instruction, err := instructions.New(instructions.Spec{
		Name:    "project",
		Source:  sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex},
		Scope:   target.ScopeProject,
	})
	if err != nil {
		t.Fatalf("instructions.New returned error: %v", err)
	}
	environment, err := New(Spec{
		Targets:      []target.Target{target.TargetCodex},
		Defaults:     testDefaults(t),
		Skills:       []skill.Skill{testSkill(t, "review", "review")},
		HookAssets:   []hookasset.HookAsset{testHookAsset(t, target.ScopeProject)},
		Instructions: []instructions.Instructions{instruction},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	sources := environment.EntityArtifactSources()
	want := []string{
		"local:skills/review?mode=vendor",
		"local:hooks/guard.sh?mode=vendor",
		"local:instructions/AGENTS.md?mode=vendor",
	}
	if len(sources) != len(want) {
		t.Fatalf("EntityArtifactSources length = %d, want %d", len(sources), len(want))
	}
	for index, sourceSpec := range sources {
		sourceID, err := source.SourceIDFor(sourceSpec)
		if err != nil {
			t.Fatalf("SourceIDFor source[%d]: %v", index, err)
		}
		if string(sourceID) != want[index] {
			t.Fatalf("source[%d] = %q, want %q", index, sourceID, want[index])
		}
	}
	sources[0] = source.Source{}
	firstID, err := source.SourceIDFor(environment.EntityArtifactSources()[0])
	if err != nil || string(firstID) != want[0] {
		t.Fatalf("EntityArtifactSources returned aliased storage: id=%q err=%v", firstID, err)
	}
}

func TestEnvironmentRejectsEntityAndSkillDestinationCollisions(t *testing.T) {
	base := Spec{Targets: []target.Target{target.TargetCodex}, Defaults: testDefaults(t)}
	base.Skills = []skill.Skill{testSkill(t, "same", "one"), testSkill(t, "same", "two")}
	if _, err := New(base); err == nil || !strings.Contains(err.Error(), `duplicate skill id "same"`) {
		t.Fatalf("duplicate entity error = %v", err)
	}
	base.Skills = []skill.Skill{testSkill(t, "one", "same"), testSkill(t, "two", "same")}
	if _, err := New(base); err == nil || !strings.Contains(err.Error(), "duplicate skill destination") {
		t.Fatalf("duplicate destination error = %v", err)
	}
}

func TestEnvironmentAllowsOrthogonalIdentityAndDestinationAxes(t *testing.T) {
	sharedNameSkill := testSkill(t, "shared", "shared")
	sharedNameHook := testHookNamed(t, "shared", "echo ok", target.ScopeProject)
	otherTarget, err := skill.New(skill.Spec{
		Name: "other", InstallName: "shared",
		Source:  sourcetest.Local(t, "skills/other", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetClaudeCode}, Scope: target.ScopeProject,
		InstallMode: skill.InstallModeCopy,
	})
	if err != nil {
		t.Fatalf("skill.New returned error: %v", err)
	}
	_, err = New(Spec{
		Targets: []target.Target{target.TargetCodex, target.TargetClaudeCode}, Defaults: testDefaults(t),
		Skills: []skill.Skill{sharedNameSkill, otherTarget}, Hooks: []hook.Hook{sharedNameHook},
	})
	if err != nil {
		t.Fatalf("New conflated family identity or disjoint target destinations: %v", err)
	}
}

func TestEnvironmentPreservesInstructionNamesButRejectsLegacyTrimCollision(t *testing.T) {
	makeInstructions := func(name string) instructions.Instructions {
		value, err := instructions.New(instructions.Spec{
			Name: name, Source: sourcetest.Local(t, "AGENTS.md", source.LocalSourceModeVendor),
			Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		})
		if err != nil {
			t.Fatalf("instructions.New returned error: %v", err)
		}
		return value
	}
	left := makeInstructions("project")
	right := makeInstructions(" project ")
	if right.ID().Name() != " project " {
		t.Fatalf("instruction identity was silently rewritten: %q", right.ID().Name())
	}
	_, err := New(Spec{
		Targets: []target.Target{target.TargetCodex}, Defaults: testDefaults(t),
		Instructions: []instructions.Instructions{left, right},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate instructions id") {
		t.Fatalf("trim collision error = %v", err)
	}
}

func TestGeneratedSkillsShareDirectIdentityAndDestinationNamespace(t *testing.T) {
	environment, err := New(Spec{
		Targets: []target.Target{target.TargetCodex}, Defaults: testDefaults(t),
		Skills: []skill.Skill{testSkill(t, "direct", "installed")},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, err := environment.WithGeneratedSkills([]skill.Skill{testSkill(t, "direct", "other")}); err == nil || !strings.Contains(err.Error(), `duplicate skill id "direct"`) {
		t.Fatalf("generated identity collision error = %v", err)
	}
	if _, err := environment.WithGeneratedSkills([]skill.Skill{testSkill(t, "generated", "installed")}); err == nil || !strings.Contains(err.Error(), "duplicate skill destination") {
		t.Fatalf("generated destination collision error = %v", err)
	}
}

func TestEnvironmentRejectsDuplicateExtensionRelationsAcrossIDs(t *testing.T) {
	sourceRef, _ := extension.NewSourceRef(extension.SourceKindMarketplace, "plugin@market")
	makeExtension := func(name string) extension.Extension {
		value, err := extension.New(extension.Spec{Name: name, Carrier: extension.CarrierClaudeCodePlugin, Target: target.TargetClaudeCode, Scope: target.ScopeProject, Source: sourceRef})
		if err != nil {
			t.Fatalf("extension.New returned error: %v", err)
		}
		return value
	}
	_, err := New(Spec{
		Targets: []target.Target{target.TargetClaudeCode}, Defaults: testDefaults(t),
		Extensions: []extension.Extension{makeExtension("left"), makeExtension("right")},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate extension relation") {
		t.Fatalf("duplicate relation error = %v", err)
	}
}

func TestEnvironmentAllowsDistinctExtensionSourceRelations(t *testing.T) {
	makeExtension := func(name string, ref string) extension.Extension {
		sourceRef, err := extension.NewSourceRef(extension.SourceKindMarketplace, ref)
		if err != nil {
			t.Fatalf("extension.NewSourceRef returned error: %v", err)
		}
		value, err := extension.New(extension.Spec{Name: name, Carrier: extension.CarrierClaudeCodePlugin, Target: target.TargetClaudeCode, Scope: target.ScopeProject, Source: sourceRef})
		if err != nil {
			t.Fatalf("extension.New returned error: %v", err)
		}
		return value
	}
	_, err := New(Spec{
		Targets: []target.Target{target.TargetClaudeCode}, Defaults: testDefaults(t),
		Extensions: []extension.Extension{makeExtension("left", "left@market"), makeExtension("right", "right@market")},
	})
	if err != nil {
		t.Fatalf("New conflated distinct extension relations: %v", err)
	}
}

func TestEnvironmentOwnsHookAssetReferenceIntegrity(t *testing.T) {
	asset := testHookAsset(t, target.ScopeProject)
	validHook := testHook(t, "python {hook_file:guard}", target.ScopeProject)
	if _, err := New(Spec{
		Targets: []target.Target{target.TargetCodex}, Defaults: testDefaults(t),
		Hooks: []hook.Hook{validHook}, HookAssets: []hookasset.HookAsset{asset},
	}); err != nil {
		t.Fatalf("valid hook asset relation rejected: %v", err)
	}
	tests := []struct {
		name   string
		value  hook.Hook
		assets []hookasset.HookAsset
		want   string
	}{
		{name: "missing", value: validHook, want: "is not declared"},
		{name: "scope mismatch", value: validHook, assets: []hookasset.HookAsset{testHookAsset(t, target.ScopeGlobal)}, want: "does not match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(Spec{Targets: []target.Target{target.TargetCodex}, Defaults: testDefaults(t), Hooks: []hook.Hook{test.value}, HookAssets: test.assets})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestEnvironmentAllowsHookWithoutAssetReferences(t *testing.T) {
	_, err := New(Spec{
		Targets: []target.Target{target.TargetCodex}, Defaults: testDefaults(t),
		Hooks: []hook.Hook{testHook(t, "echo ok", target.ScopeProject)},
	})
	if err != nil {
		t.Fatalf("New required an unused hook asset: %v", err)
	}
}

func TestEnvironmentRejectsZeroFamilyAndZeroEnvironment(t *testing.T) {
	_, err := New(Spec{Targets: []target.Target{target.TargetCodex}, Defaults: testDefaults(t), Skills: []skill.Skill{{}}})
	if err == nil || !strings.Contains(err.Error(), "entity kind") {
		t.Fatalf("zero family error = %v", err)
	}
	if err := (Environment{}).Validate(); err == nil {
		t.Fatal("zero Environment validated")
	}
}

func TestEnvironmentRejectsDuplicateMCPServerIdentityAcrossBindingSets(t *testing.T) {
	command, _ := mcp.NewAmbientCommand("node")
	transport, _ := mcp.NewStdioTransport(command, nil, nil)
	project, _ := mcp.NewBinding(target.TargetCodex, target.ScopeProject, transport, mcp.OnAbsentRemoveBinding)
	global, _ := mcp.NewBinding(target.TargetCodex, target.ScopeGlobal, transport, mcp.OnAbsentRemoveBinding)
	left, _ := mcp.New(mcp.Spec{Name: "server", Bindings: []mcp.Binding{project}})
	right, _ := mcp.New(mcp.Spec{Name: "server", Bindings: []mcp.Binding{global}})
	_, err := New(Spec{
		Targets: []target.Target{target.TargetCodex}, Defaults: testDefaults(t),
		MCPServers: []mcp.Server{left, right},
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate mcp_server id "server"`) {
		t.Fatalf("duplicate MCP identity error = %v", err)
	}
}

func testDefaults(t *testing.T) Defaults {
	t.Helper()
	value, err := NewDefaults(target.ScopeProject, skill.InstallModeCopy)
	if err != nil {
		t.Fatalf("NewDefaults returned error: %v", err)
	}
	return value
}

func testSkill(t *testing.T, name string, installName string) skill.Skill {
	t.Helper()
	value, err := skill.New(skill.Spec{
		Name: name, InstallName: installName,
		Source:  sourcetest.Local(t, "skills/"+name, source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		InstallMode: skill.InstallModeCopy,
	})
	if err != nil {
		t.Fatalf("skill.New returned error: %v", err)
	}
	return value
}

func testHook(t *testing.T, command string, scope target.Scope) hook.Hook {
	return testHookNamed(t, "protect", command, scope)
}

func testHookNamed(t *testing.T, name string, command string, scope target.Scope) hook.Hook {
	t.Helper()
	value, err := hook.New(hook.Spec{
		Name: name, Event: "Stop", Type: hook.TypeCommand, Command: command,
		Targets: []target.Target{target.TargetCodex}, Scope: scope,
	})
	if err != nil {
		t.Fatalf("hook.New returned error: %v", err)
	}
	return value
}

func testHookAsset(t *testing.T, scope target.Scope) hookasset.HookAsset {
	t.Helper()
	path := "hooks/guard.sh"
	if scope == target.ScopeGlobal {
		path = "/tmp/guard.sh"
	}
	value, err := hookasset.New(hookasset.Spec{
		Name: "guard", Source: sourcetest.Local(t, path, source.LocalSourceModeVendor),
		ArtifactKind: hookasset.ArtifactKindFile, Scope: scope, Executable: true,
	})
	if err != nil {
		t.Fatalf("hookasset.New returned error: %v", err)
	}
	return value
}
