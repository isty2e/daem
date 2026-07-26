package generate

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	hookassetresource "github.com/isty2e/daem/internal/desired/hookasset"
	instructionsresource "github.com/isty2e/daem/internal/desired/instructions"
	skillresource "github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestConsumedLocalPathsCoversEveryLocalSnapshotInput(t *testing.T) {
	root := t.TempDir()
	shared := sourcetest.Local(t, "shared", source.LocalSourceModeVendor)
	other := sourcetest.Local(t, "other", source.LocalSourceModeVendor)
	gitSource, err := source.NewGitSource("https://example.test/repo.git", ".", "main")
	if err != nil {
		t.Fatal(err)
	}

	environment := desiredtest.Environment(t, desired.Spec{
		Targets:  []target.Target{target.TargetCodex},
		Defaults: desiredtest.Defaults(t, target.ScopeProject, skillresource.InstallModeCopy),
		Skills: []skillresource.Skill{
			desiredtest.Skill(t, skillresource.Spec{Name: "shared", Source: shared, Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject, InstallMode: skillresource.InstallModeCopy}),
			desiredtest.Skill(t, skillresource.Spec{Name: "other", Source: other, Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject, InstallMode: skillresource.InstallModeCopy}),
			desiredtest.Skill(t, skillresource.Spec{Name: "git", Source: gitSource, Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject, InstallMode: skillresource.InstallModeCopy}),
		},
		Instructions: []instructionsresource.Instructions{
			desiredtest.Instructions(t, instructionsresource.Spec{Name: "instructions", Source: shared, Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject}),
		},
		HookAssets: []hookassetresource.HookAsset{
			desiredtest.HookAsset(t, hookassetresource.Spec{Name: "hook", Source: sourcetest.Local(t, "hooks/run.sh", source.LocalSourceModeVendor), ArtifactKind: hookassetresource.ArtifactKindFile, Scope: target.ScopeProject}),
		},
		SkillSets: []skillresource.SkillSet{
			desiredtest.SkillSet(t, skillresource.SkillSetSpec{
				Source:  sourcetest.Local(t, "groups", source.LocalSourceModeVendor),
				Include: []skillresource.Selector{desiredtest.Selector(t, "glob:*")},
				Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
				InstallMode: skillresource.InstallModeCopy,
			}),
		},
	})
	got, err := ConsumedLocalPaths(Input{
		Paths:       paths.Paths{ManifestRoot: root},
		Environment: environment,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "groups"),
		filepath.Join(root, "hooks", "run.sh"),
		filepath.Join(root, "other"),
		filepath.Join(root, "shared"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConsumedLocalPaths() = %#v, want %#v", got, want)
	}
}
