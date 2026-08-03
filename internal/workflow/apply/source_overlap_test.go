package apply

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/skill"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestPathsOverlapUsesPathComponentsInBothDirections(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source")
	tests := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "equal", left: source, right: source, want: true},
		{name: "source contains destination", left: source, right: filepath.Join(source, "child"), want: true},
		{name: "destination contains source", left: filepath.Join(source, "child"), right: source, want: true},
		{name: "sibling", left: source, right: filepath.Join(root, "other")},
		{name: "text prefix only", left: source, right: source + "-other"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pathsOverlap(test.left, test.right); got != test.want {
				t.Fatalf("pathsOverlap(%q, %q) = %t, want %t", test.left, test.right, got, test.want)
			}
		})
	}
}

func TestLocalEntityArtifactSourceAuthorityPathsCoalesceSharedSources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	shared := sourcetest.Local(t, "shared", source.LocalSourceModeVendor)
	resources := make([]skill.Skill, 0, 2)
	for _, name := range []string{"alpha", "beta"} {
		value, err := skill.New(skill.Spec{
			Name: name, InstallName: name, Source: shared,
			Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
			InstallMode: skill.InstallModeCopy,
		})
		if err != nil {
			t.Fatalf("skill.New %q: %v", name, err)
		}
		resources = append(resources, value)
	}
	defaults, err := desired.NewDefaults(target.ScopeProject, skill.InstallModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := desired.New(desired.Spec{
		Targets: []target.Target{target.TargetCodex}, Defaults: defaults, Skills: resources,
	})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := localEntityArtifactSourceAuthorityPaths(paths, environment)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "shared")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("localEntityArtifactSourceAuthorityPaths = %#v, want [%q]", got, want)
	}
}
