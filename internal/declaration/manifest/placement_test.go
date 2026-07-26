package manifest

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	desiredhookasset "github.com/isty2e/daem/internal/desired/hookasset"
	desiredinstructions "github.com/isty2e/daem/internal/desired/instructions"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/desired/testfixture"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestValidatePlacementRejectsProjectResourcesFromUserDefaultManifest(t *testing.T) {
	root := t.TempDir()
	paths := daempaths.Paths{
		ManifestPath:   filepath.Join(root, "config", "daem", "daem.toml"),
		ManifestRoot:   filepath.Join(root, "config", "daem"),
		ManifestOrigin: daempaths.ManifestOriginUserDefault,
	}
	cases := []struct {
		family string
		want   string
	}{
		{family: "instructions", want: `project-scoped instruction "project" requires a project manifest`},
		{family: "skill", want: `project-scoped skill "review" requires a project manifest`},
		{family: "skill_set", want: `project-scoped skill_group[0] requires a project manifest`},
		{family: "hook", want: `project-scoped hook "guard" requires a project manifest`},
		{family: "hook_asset", want: `project-scoped hook_asset "guard" requires a project manifest`},
		{family: "mcp", want: `project-scoped mcp_server "repo-tools" requires a project manifest`},
		{family: "claude_extension", want: `project-scoped extension "claude-tools" requires a project manifest`},
		{family: "opencode_extension", want: `project-scoped extension "opencode-tools" requires a project manifest`},
		{family: "pi_extension", want: `project-scoped extension "pi-tools" requires a project manifest`},
	}

	for _, test := range cases {
		t.Run(test.family, func(t *testing.T) {
			err := ValidatePlacement(paths, placementEnvironment(t, test.family, target.ScopeProject))
			if err == nil {
				t.Fatal("ValidatePlacement returned nil error")
			}
			if !strings.Contains(err.Error(), test.want) ||
				!strings.Contains(err.Error(), "use --manifest ./daem.toml") ||
				!strings.Contains(err.Error(), `scope = "global"`) {
				t.Fatalf("error = %q, want placement guidance", err)
			}
		})
	}
}

func TestValidatePlacementAllowsGlobalResourcesFromUserDefaultManifest(t *testing.T) {
	root := t.TempDir()
	paths := daempaths.Paths{
		ManifestPath:   filepath.Join(root, "config", "daem", "daem.toml"),
		ManifestRoot:   filepath.Join(root, "config", "daem"),
		ManifestOrigin: daempaths.ManifestOriginUserDefault,
	}
	for _, family := range []string{"instructions", "skill", "skill_set", "hook", "hook_asset", "mcp", "claude_extension", "opencode_extension", "pi_extension"} {
		t.Run(family, func(t *testing.T) {
			if err := ValidatePlacement(paths, placementEnvironment(t, family, target.ScopeGlobal)); err != nil {
				t.Fatalf("ValidatePlacement returned error: %v", err)
			}
		})
	}
}

func TestValidatePlacementAllowsProjectResourcesFromProjectManifest(t *testing.T) {
	root := t.TempDir()
	paths := daempaths.Paths{ManifestPath: filepath.Join(root, "daem.toml"), ManifestRoot: root, ManifestOrigin: daempaths.ManifestOriginCWD}
	for _, family := range []string{"instructions", "skill", "skill_set", "hook", "hook_asset", "mcp", "claude_extension", "opencode_extension", "pi_extension"} {
		t.Run(family, func(t *testing.T) {
			if err := ValidatePlacement(paths, placementEnvironment(t, family, target.ScopeProject)); err != nil {
				t.Fatalf("ValidatePlacement returned error: %v", err)
			}
		})
	}
}

func placementEnvironment(t *testing.T, family string, scope target.Scope) desired.Environment {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), family)
	defaults := testfixture.Defaults(t, scope, desiredskill.InstallModeCopy)
	spec := desired.Spec{Defaults: defaults}

	switch family {
	case "instructions":
		spec.Targets = []target.Target{target.TargetCodex}
		spec.Instructions = []desiredinstructions.Instructions{testfixture.Instructions(t, desiredinstructions.Spec{
			Name: "project", Source: sourcetest.Local(t, sourcePath, source.LocalSourceModeVendor),
			Targets: spec.Targets, Scope: scope,
		})}
	case "skill":
		spec.Targets = []target.Target{target.TargetCodex}
		spec.Skills = []desiredskill.Skill{testfixture.Skill(t, desiredskill.Spec{
			Name: "review", Source: sourcetest.Local(t, sourcePath, source.LocalSourceModeVendor),
			Targets: spec.Targets, Scope: scope, InstallMode: desiredskill.InstallModeCopy, Portable: true,
		})}
	case "skill_set":
		spec.Targets = []target.Target{target.TargetCodex}
		spec.SkillSets = []desiredskill.SkillSet{testfixture.SkillSet(t, desiredskill.SkillSetSpec{
			Source: sourcetest.Local(t, sourcePath, source.LocalSourceModeVendor), Include: []desiredskill.Selector{testfixture.Selector(t, "glob:*")},
			Targets: spec.Targets, Scope: scope, InstallMode: desiredskill.InstallModeCopy, Portable: true,
		})}
	case "hook":
		spec.Targets = []target.Target{target.TargetCodex}
		spec.Hooks = []desiredhook.Hook{testfixture.Hook(t, desiredhook.Spec{
			Name: "guard", Event: "Stop", Type: desiredhook.TypeCommand, Command: "echo guard", Targets: spec.Targets, Scope: scope,
		})}
	case "hook_asset":
		spec.Targets = []target.Target{target.TargetCodex}
		spec.HookAssets = []desiredhookasset.HookAsset{testfixture.HookAsset(t, desiredhookasset.Spec{
			Name: "guard", Source: sourcetest.Local(t, sourcePath, source.LocalSourceModeVendor), ArtifactKind: desiredhookasset.ArtifactKindFile, Scope: scope,
		})}
	case "mcp":
		spec.Targets = []target.Target{target.TargetClaudeCode}
		transport := testfixture.MCPStdio(t, testfixture.MCPCommand(t, "npx"), nil, nil)
		binding := testfixture.MCPBinding(t, target.TargetClaudeCode, scope, transport, desiredmcp.OnAbsentRemoveBinding)
		spec.MCPServers = []desiredmcp.Server{testfixture.MCPServer(t, desiredmcp.Spec{Name: "repo-tools", Bindings: []desiredmcp.Binding{binding}})}
	case "claude_extension":
		spec.Targets = []target.Target{target.TargetClaudeCode}
		spec.Extensions = []desiredextension.Extension{placementExtension(t, "claude-tools", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, scope, desiredextension.SourceKindMarketplace, "tools@market")}
	case "opencode_extension":
		spec.Targets = []target.Target{target.TargetOpenCode}
		spec.Extensions = []desiredextension.Extension{placementExtension(t, "opencode-tools", desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, scope, desiredextension.SourceKindHostSource, "@acme/opencode-tools")}
	case "pi_extension":
		spec.Targets = []target.Target{target.TargetPi}
		spec.Extensions = []desiredextension.Extension{placementExtension(t, "pi-tools", desiredextension.CarrierPiPackage, target.TargetPi, scope, desiredextension.SourceKindHostSource, "github:acme/pi-tools")}
	default:
		t.Fatalf("unknown placement family %q", family)
	}
	return testfixture.Environment(t, spec)
}

func placementExtension(
	t *testing.T,
	name string,
	carrier desiredextension.Carrier,
	selected target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
) desiredextension.Extension {
	t.Helper()
	return testfixture.Extension(t, desiredextension.Spec{
		Name: name, Carrier: carrier, Target: selected, Scope: scope,
		Source: testfixture.ExtensionSource(t, sourceKind, sourceRef),
	})
}
