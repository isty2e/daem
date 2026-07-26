package skill

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestSkillSetExpandConsumesMatchingCanonicalListing(t *testing.T) {
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	set := mustExpansionSkillSet(t, SkillSetSpec{
		Source:       root,
		Include:      []Selector{mustSelector(t, "glob:*")},
		Exclude:      []Selector{mustSelector(t, "glob:draft-*")},
		Targets:      []target.Target{target.TargetCodex},
		Scope:        target.ScopeProject,
		InstallMode:  InstallModeCopy,
		Portable:     true,
		CompatRepair: true,
	})
	listing, err := source.NewRootListing(
		root,
		"",
		artifact.ArtifactKindDirectory,
		[]string{"zeta", "draft-one", "alpha"},
	)
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}

	children, err := set.Expand(listing)
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}
	if got := []string{children[0].ID().Name(), children[1].ID().Name()}; !slices.Equal(got, []string{"alpha", "zeta"}) {
		t.Fatalf("expanded children = %v", got)
	}
	for _, child := range children {
		local, ok := child.Source().Local()
		if !ok || local.Path() != "skills/"+child.ID().Name() {
			t.Fatalf("child source = %#v", child.Source())
		}
		if !child.Portable() || !child.CompatRepair() || child.InstallName() != child.ID().Name() {
			t.Fatalf("child policy = %#v", child)
		}
	}
}

func TestSkillSetExpandRejectsMismatchedOrNonDirectoryListing(t *testing.T) {
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	set := mustExpansionSkillSet(t, baseExpansionSkillSetSpec(root))
	otherRoot := sourcetest.Local(t, "other", source.LocalSourceModeVendor)
	mismatched, err := source.NewRootListing(otherRoot, "", artifact.ArtifactKindDirectory, []string{"alpha"})
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}
	if _, err := set.Expand(mismatched); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Expand mismatch error = %v", err)
	}

	fileListing, err := source.NewRootListing(root, "", artifact.ArtifactKindFile, nil)
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}
	if _, err := set.Expand(fileListing); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("Expand file error = %v", err)
	}
}

func TestSkillSetExpandKeepsSourceComponentAndSkillIdentityValidationDistinct(t *testing.T) {
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	set := mustExpansionSkillSet(t, baseExpansionSkillSetSpec(root))
	listing, err := source.NewRootListing(root, "", artifact.ArtifactKindDirectory, []string{"~source-valid-but-not-skill"})
	if err != nil {
		t.Fatalf("NewRootListing rejected source-safe component: %v", err)
	}
	if _, err := set.Expand(listing); err == nil || !strings.Contains(err.Error(), "safe single path segment") {
		t.Fatalf("Expand error = %v, want Skill identity rejection", err)
	}
}

func TestSkillSetExpandRejectsEmptySelectionAfterExclusion(t *testing.T) {
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	spec := baseExpansionSkillSetSpec(root)
	spec.Exclude = []Selector{mustSelector(t, "regex:^(alpha|beta)$")}
	set := mustExpansionSkillSet(t, spec)
	listing, err := source.NewRootListing(
		root,
		"",
		artifact.ArtifactKindDirectory,
		[]string{"alpha", "beta"},
	)
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}

	if _, err := set.Expand(listing); err == nil || err.Error() != "include: selectors matched no skills after exclusions" {
		t.Fatalf("Expand error = %v, want empty-after-exclusion diagnostic", err)
	}
}

func baseExpansionSkillSetSpec(root source.Source) SkillSetSpec {
	return SkillSetSpec{
		Source:      root,
		Include:     []Selector{{kind: SelectorGlob, pattern: "*"}},
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: InstallModeCopy,
	}
}

func mustExpansionSkillSet(t *testing.T, spec SkillSetSpec) SkillSet {
	t.Helper()
	set, err := NewSkillSet(spec)
	if err != nil {
		t.Fatalf("NewSkillSet returned error: %v", err)
	}
	return set
}
