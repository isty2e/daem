package diagnose_test

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	desiredinstructions "github.com/isty2e/daem/internal/desired/instructions"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/diagnose"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestNewDerivesExactDoctorManifestProjection(t *testing.T) {
	facts, err := diagnose.NewManifestFacts(testManifest(t))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	wantTargets := []target.Target{target.TargetCodex, target.TargetClaudeCode, target.TargetOpenCode, target.TargetAntigravityCLI}
	if got := facts.ContextSelection().Targets(); !reflect.DeepEqual(got, wantTargets) {
		t.Fatalf("ContextSelection targets = %#v, want %#v", got, wantTargets)
	}
	wantKinds := map[entity.Kind]struct{}{
		entity.KindInstructions: {}, entity.KindSkill: {}, entity.KindHook: {},
	}
	if got := facts.ResourceKinds(); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("ResourceKinds = %#v, want %#v", got, wantKinds)
	}
	if got := facts.Skills(); len(got) != 1 || got[0].ID().Name() != "demo-skill" {
		t.Fatalf("Skills = %#v, want demo-skill", got)
	}
	if got := facts.Hooks(); len(got) != 1 || got[0].ID().Name() != "stop-hook" {
		t.Fatalf("Hooks = %#v, want stop-hook", got)
	}
	if got := facts.SkillSets(); len(got) != 1 || got[0].Include()[0].Pattern() != "team-*" {
		t.Fatalf("SkillSets = %#v, want team-* selector", got)
	}
	if got := facts.MCPServers(); len(got) != 1 || got[0].ID().Name() != "repo-tools" {
		t.Fatalf("MCPServers = %#v, want repo-tools", got)
	}
}

func TestFactsAccessorsReturnIsolatedCollections(t *testing.T) {
	facts, err := diagnose.NewManifestFacts(testManifest(t))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	targets := facts.ContextSelection().Targets()
	targets[0] = target.TargetPi
	kinds := facts.ResourceKinds()
	delete(kinds, entity.KindSkill)
	skills := facts.Skills()
	skills[0] = desiredskill.Skill{}
	hooks := facts.Hooks()
	hooks[0] = desiredhook.Hook{}
	sets := facts.SkillSets()
	sets[0] = desiredskill.SkillSet{}
	servers := facts.MCPServers()
	servers[0] = desiredmcp.Server{}

	if facts.ContextSelection().Targets()[0] != target.TargetCodex {
		t.Fatal("context targets changed through accessor alias")
	}
	if _, ok := facts.ResourceKinds()[entity.KindSkill]; !ok {
		t.Fatal("resource kinds changed through accessor alias")
	}
	if facts.Skills()[0].ID().Name() != "demo-skill" || facts.Hooks()[0].ID().Name() != "stop-hook" ||
		facts.SkillSets()[0].Include()[0].Pattern() != "team-*" || facts.MCPServers()[0].ID().Name() != "repo-tools" {
		t.Fatal("canonical facts changed through collection accessor alias")
	}
}

func TestFactsConcurrentAccessRemainsIsolated(t *testing.T) {
	facts, err := diagnose.NewManifestFacts(testManifest(t))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	var waitGroup sync.WaitGroup
	for range 32 {
		waitGroup.Go(func() {
			facts.ResourceKinds()[entity.Kind("mcp_server")] = struct{}{}
			facts.Skills()[0] = desiredskill.Skill{}
			facts.Hooks()[0] = desiredhook.Hook{}
			facts.SkillSets()[0] = desiredskill.SkillSet{}
			facts.MCPServers()[0] = desiredmcp.Server{}
		})
	}
	waitGroup.Wait()
	if _, ok := facts.ResourceKinds()[entity.Kind("mcp_server")]; ok {
		t.Fatal("concurrent accessor mutation added MCP resource kind")
	}
	if facts.Skills()[0].ID().Name() != "demo-skill" || facts.Hooks()[0].ID().Name() != "stop-hook" ||
		facts.SkillSets()[0].Include()[0].Pattern() != "team-*" || facts.MCPServers()[0].ID().Name() != "repo-tools" {
		t.Fatal("concurrent accessor mutation changed canonical facts")
	}
}

func TestNewPreservesEmptyAndNarrowContextSemantics(t *testing.T) {
	tests := []struct {
		name        string
		environment desired.Environment
		want        []target.Target
	}{
		{name: "all-target manifest selects all supported targets", environment: testEnvironment(t, desired.Spec{Targets: target.SupportedTargets()}), want: target.SupportedTargets()},
		{name: "skill-set-only target does not narrow doctor context", environment: skillSetOnlyEnvironment(t), want: target.SupportedTargets()},
		{name: "MCP-only manifest selects its binding target", environment: mcpOnlyEnvironment(t), want: []target.Target{target.TargetClaudeCode}},
		{name: "overlapping context targets collapse in supported order", environment: overlappingTargetEnvironment(t), want: []target.Target{target.TargetCodex, target.TargetClaudeCode}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts, err := diagnose.NewManifestFacts(test.environment)
			if err != nil {
				t.Fatalf("New returned error: %v", err)
			}
			if got := facts.ContextSelection().Targets(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ContextSelection targets = %#v, want %#v", got, test.want)
			}
			if test.name == "MCP-only manifest selects its binding target" && len(facts.ResourceKinds()) != 0 {
				t.Fatalf("MCP-only ResourceKinds = %#v, want empty", facts.ResourceKinds())
			}
		})
	}
}

func TestNewRejectsZeroEnvironment(t *testing.T) {
	_, err := diagnose.NewManifestFacts(desired.Environment{})
	if err == nil || !strings.Contains(err.Error(), "environment targets") {
		t.Fatalf("New error = %v, want invalid canonical environment", err)
	}
}

func testManifest(t *testing.T) desired.Environment {
	t.Helper()
	skill := testfixture.Skill(t, desiredskill.Spec{
		Name: "demo-skill", Source: sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetClaudeCode}, Scope: target.ScopeProject, InstallMode: desiredskill.InstallModeCopy, Portable: true,
	})
	set := testfixture.SkillSet(t, desiredskill.SkillSetSpec{
		Source:  sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
		Include: []desiredskill.Selector{testfixture.Selector(t, "glob:team-*")}, Exclude: []desiredskill.Selector{testfixture.Selector(t, "glob:team-secret")},
		Targets: []target.Target{target.TargetPi}, Scope: target.ScopeProject, InstallMode: desiredskill.InstallModeCopy, Portable: true,
	})
	hook := testfixture.Hook(t, desiredhook.Spec{
		Name: "stop-hook", Event: "Stop", Type: desiredhook.TypeCommand, Command: "echo stop",
		Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
		TargetOverrides: map[target.Target]desiredhook.TargetOverride{target.TargetOpenCode: desiredhook.NewTargetOverride("", "tool")},
	})
	instruction := testfixture.Instructions(t, desiredinstructions.Spec{
		Name: "guide", Source: sourcetest.Local(t, "AGENTS.md", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetAntigravityCLI}, Scope: target.ScopeProject,
		Renderings: map[target.Target]desiredinstructions.Rendering{target.TargetAntigravityCLI: testfixture.Rendering(t, "", desiredinstructions.RenderModeCopy)},
	})
	transport := testfixture.MCPStdio(t, testfixture.MCPCommand(t, "node"), []string{"--stdio"}, map[string]desiredmcp.EnvReference{
		"TOKEN": testfixture.MCPEnvReference(t, "MCP_TOKEN"),
	})
	binding := testfixture.MCPBinding(t, target.TargetClaudeCode, target.ScopeProject, transport, desiredmcp.OnAbsentRemoveBinding)
	server := testfixture.MCPServer(t, desiredmcp.Spec{Name: "repo-tools", Bindings: []desiredmcp.Binding{binding}})
	return testEnvironment(t, desired.Spec{
		Targets: []target.Target{target.TargetCodex}, Skills: []desiredskill.Skill{skill}, SkillSets: []desiredskill.SkillSet{set},
		Hooks: []desiredhook.Hook{hook}, Instructions: []desiredinstructions.Instructions{instruction}, MCPServers: []desiredmcp.Server{server},
	})
}

func testEnvironment(t *testing.T, spec desired.Spec) desired.Environment {
	t.Helper()
	spec.Defaults = testfixture.Defaults(t, target.ScopeProject, desiredskill.InstallModeCopy)
	return testfixture.Environment(t, spec)
}

func skillSetOnlyEnvironment(t *testing.T) desired.Environment {
	t.Helper()
	set := testfixture.SkillSet(t, desiredskill.SkillSetSpec{
		Source: sourcetest.Local(t, "skills", source.LocalSourceModeVendor), Include: []desiredskill.Selector{testfixture.Selector(t, "glob:*")},
		Targets: []target.Target{target.TargetPi}, Scope: target.ScopeProject, InstallMode: desiredskill.InstallModeCopy, Portable: true,
	})
	return testEnvironment(t, desired.Spec{Targets: target.SupportedTargets(), SkillSets: []desiredskill.SkillSet{set}})
}

func mcpOnlyEnvironment(t *testing.T) desired.Environment {
	t.Helper()
	transport := testfixture.MCPStdio(t, testfixture.MCPCommand(t, "node"), nil, nil)
	binding := testfixture.MCPBinding(t, target.TargetClaudeCode, target.ScopeProject, transport, desiredmcp.OnAbsentKeep)
	server := testfixture.MCPServer(t, desiredmcp.Spec{Name: "tools", Bindings: []desiredmcp.Binding{binding}})
	return testEnvironment(t, desired.Spec{Targets: []target.Target{target.TargetClaudeCode}, MCPServers: []desiredmcp.Server{server}})
}

func overlappingTargetEnvironment(t *testing.T) desired.Environment {
	t.Helper()
	skill := testfixture.Skill(t, desiredskill.Spec{
		Name: "overlap", Source: sourcetest.Local(t, "skills/overlap", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject, InstallMode: desiredskill.InstallModeCopy, Portable: true,
	})
	return testEnvironment(t, desired.Spec{Targets: []target.Target{target.TargetClaudeCode, target.TargetCodex}, Skills: []desiredskill.Skill{skill}})
}
