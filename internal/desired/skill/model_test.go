package skill

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestSkillConstructorOwnsCanonicalInvariantsAndStorage(t *testing.T) {
	targets := []target.Target{target.TargetCodex, target.TargetClaudeCode}
	skill, err := New(Spec{
		Name:         " review ",
		Source:       sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor),
		Targets:      targets,
		Scope:        target.ScopeProject,
		InstallMode:  InstallModeCopy,
		Portable:     true,
		CompatRepair: true,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	targets[0] = target.TargetPi
	if skill.ID().Kind() != entity.KindSkill || skill.ID().Name() != "review" || skill.InstallName() != "review" {
		t.Fatalf("skill identity = %s install=%q", skill.ID(), skill.InstallName())
	}
	gotTargets := skill.Targets()
	if !slices.Equal(gotTargets, []target.Target{target.TargetCodex, target.TargetClaudeCode}) {
		t.Fatalf("Targets = %#v", gotTargets)
	}
	gotTargets[0] = target.TargetPi
	if skill.Targets()[0] != target.TargetCodex {
		t.Fatal("Targets returned aliased storage")
	}
	if !skill.Portable() || !skill.CompatRepair() || skill.Validate() != nil {
		t.Fatalf("skill policy or validation mismatch")
	}
}

func TestSkillEqualCoversCanonicalDeclarationSemantics(t *testing.T) {
	placement, err := NewTargetPlacement(target.ScopeProject, ".codex/skills")
	if err != nil {
		t.Fatal(err)
	}
	base := Spec{
		Name:         "review",
		InstallName:  "review",
		Source:       sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor),
		Targets:      []target.Target{target.TargetCodex, target.TargetClaudeCode},
		Placements:   map[target.Target]TargetPlacement{target.TargetCodex: placement},
		Scope:        target.ScopeProject,
		InstallMode:  InstallModeCopy,
		Portable:     true,
		CompatRepair: false,
	}
	mustSkill := func(spec Spec) Skill {
		t.Helper()
		value, newErr := New(spec)
		if newErr != nil {
			t.Fatal(newErr)
		}
		return value
	}

	want := mustSkill(base)
	reordered := base
	reordered.Targets = []target.Target{target.TargetClaudeCode, target.TargetCodex}
	if !want.Equal(mustSkill(reordered)) {
		t.Fatal("target set order changed canonical Skill equality")
	}

	variants := []Spec{}
	name := base
	name.Name = "other"
	variants = append(variants, name)
	installName := base
	installName.InstallName = "other"
	variants = append(variants, installName)
	skillSource := base
	skillSource.Source = sourcetest.Local(t, "skills/other", source.LocalSourceModeVendor)
	variants = append(variants, skillSource)
	targets := base
	targets.Targets = []target.Target{target.TargetCodex}
	variants = append(variants, targets)
	placements := base
	placements.Placements = nil
	variants = append(variants, placements)
	installMode := base
	installMode.InstallMode = InstallModeSymlink
	variants = append(variants, installMode)
	portable := base
	portable.Portable = false
	variants = append(variants, portable)
	compatRepair := base
	compatRepair.CompatRepair = true
	variants = append(variants, compatRepair)

	for index, variant := range variants {
		if want.Equal(mustSkill(variant)) {
			t.Fatalf("variant %d compared equal to canonical Skill", index)
		}
	}
	differentScope := want
	differentScope.scope = target.ScopeGlobal
	if want.Equal(differentScope) {
		t.Fatal("scope drift compared equal to canonical Skill")
	}
}

func TestSkillConstructorRejectsCrossAxisInvalidStates(t *testing.T) {
	base := Spec{
		Name:        "review",
		Source:      sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor),
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: InstallModeCopy,
	}
	tests := []struct {
		name string
		edit func(*Spec)
		want string
	}{
		{name: "unsafe id", edit: func(spec *Spec) { spec.Name = "../review" }, want: "safe single path"},
		{name: "unsafe install", edit: func(spec *Spec) { spec.InstallName = "review/sub" }, want: "safe single path"},
		{name: "no targets", edit: func(spec *Spec) { spec.Targets = nil }, want: "at least one target"},
		{name: "bad scope", edit: func(spec *Spec) { spec.Scope = "workspace" }, want: "unknown scope"},
		{name: "bad mode", edit: func(spec *Spec) { spec.InstallMode = "mirror" }, want: "unknown install mode"},
		{name: "global relative local", edit: func(spec *Spec) { spec.Scope = target.ScopeGlobal }, want: "absolute path"},
		{name: "portable project link", edit: func(spec *Spec) {
			spec.Source = sourcetest.Local(t, "skills/review", source.LocalSourceModeLink)
			spec.Portable = true
		}, want: "portable = false"},
		{name: "s3 file", edit: func(spec *Spec) {
			spec.Source = sourcetest.S3(t, "s3://bucket/review", "", "", source.S3ObjectFormatFile)
		}, want: "archive format"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := base
			test.edit(&spec)
			if _, err := New(spec); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New error = %v, want containing %q", err, test.want)
			}
		})
	}
	if err := (Skill{}).Validate(); err == nil {
		t.Fatal("zero Skill validated")
	}
}

func TestSkillConstructorRejectsEmbeddedControlCharactersInNames(t *testing.T) {
	for _, name := range []string{"review\x00hidden", "review\nhidden", "review\x7fhidden", "review\u0085hidden", "safe\u202etxt"} {
		_, err := New(Spec{
			Name: name, Source: sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor),
			Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
			InstallMode: InstallModeCopy,
		})
		if err == nil {
			t.Fatalf("New accepted control-bearing skill name %q", name)
		}
	}
}

func TestSkillNamesRejectInvalidUTF8FromSourceListings(t *testing.T) {
	invalid := string([]byte{'r', 'e', 'v', 'i', 'e', 'w', 0xff})
	if _, err := New(Spec{
		Name: invalid, Source: sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		InstallMode: InstallModeCopy,
	}); err == nil {
		t.Fatal("New accepted an invalid-UTF-8 skill identity")
	}

	set, err := NewSkillSet(SkillSetSpec{
		Source:  sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
		Include: []Selector{mustSelector(t, "glob:*")}, Targets: []target.Target{target.TargetCodex},
		Scope: target.ScopeProject, InstallMode: InstallModeCopy,
	})
	if err != nil {
		t.Fatalf("NewSkillSet returned error: %v", err)
	}
	if _, err := set.Select([]string{invalid}); err == nil {
		t.Fatal("Select accepted an invalid-UTF-8 directory entry")
	}
}

func TestSkillSetSelectsDeterministicallyAndBuildsCanonicalChildren(t *testing.T) {
	include := []Selector{mustSelector(t, "glob:*")}
	exclude := []Selector{mustSelector(t, "glob:draft-*")}
	set, err := NewSkillSet(SkillSetSpec{
		Source:       sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
		Include:      include,
		Exclude:      exclude,
		Targets:      []target.Target{target.TargetCodex},
		Scope:        target.ScopeProject,
		InstallMode:  InstallModeCopy,
		Portable:     true,
		CompatRepair: true,
	})
	if err != nil {
		t.Fatalf("NewSkillSet returned error: %v", err)
	}
	include[0] = mustSelector(t, "glob:missing")
	exclude[0] = mustSelector(t, "glob:*")

	names, err := set.Select([]string{"zeta", "draft-one", "alpha"})
	if err != nil {
		t.Fatalf("Select returned error: %v", err)
	}
	if !slices.Equal(names, []string{"alpha", "zeta"}) {
		t.Fatalf("Select = %#v", names)
	}
	child, err := set.Child("alpha", sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor))
	if err != nil {
		t.Fatalf("Child returned error: %v", err)
	}
	if child.ID().Name() != "alpha" || !child.CompatRepair() || !child.Portable() {
		t.Fatalf("child = %#v", child)
	}
	if _, err := set.Child("draft-one", sourcetest.Local(t, "skills/draft-one", source.LocalSourceModeVendor)); err == nil {
		t.Fatal("excluded child was constructed")
	}
	if _, err := set.Child("alpha", sourcetest.Local(t, "other/alpha", source.LocalSourceModeVendor)); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("unrelated child source error = %v", err)
	}
}

func TestSkillSetRejectsInvalidGeneratorStates(t *testing.T) {
	base := SkillSetSpec{
		Source:      sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
		Include:     []Selector{mustSelector(t, "glob:*")},
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: InstallModeCopy,
	}
	tests := []struct {
		name string
		edit func(*SkillSetSpec)
		want string
	}{
		{name: "missing selectors", edit: func(spec *SkillSetSpec) { spec.Include = nil }, want: "include selectors"},
		{name: "s3 root", edit: func(spec *SkillSetSpec) {
			spec.Source = sourcetest.S3(t, "s3://bucket/root", "", "", source.S3ObjectFormatTar)
		}, want: "S3 skill sets"},
		{name: "global relative root", edit: func(spec *SkillSetSpec) { spec.Scope = target.ScopeGlobal }, want: "absolute path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := base
			test.edit(&spec)
			if _, err := NewSkillSet(spec); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewSkillSet error = %v, want containing %q", err, test.want)
			}
		})
	}
	if err := (SkillSet{}).Validate(); err == nil {
		t.Fatal("zero SkillSet validated")
	}
	if _, err := NewSkillSet(base); err != nil {
		t.Fatalf("base NewSkillSet error: %v", err)
	}
}

func TestSkillSetRejectsAdversarialListingFacts(t *testing.T) {
	set, err := NewSkillSet(SkillSetSpec{
		Source:      sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
		Include:     []Selector{mustSelector(t, "glob:*")},
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: InstallModeCopy,
	})
	if err != nil {
		t.Fatalf("NewSkillSet returned error: %v", err)
	}
	for _, names := range [][]string{{"alpha", "alpha"}, {" alpha "}, {"../alpha"}} {
		if _, err := set.Select(names); err == nil {
			t.Fatalf("Select(%#v) returned nil error", names)
		}
	}
}

func TestSkillSetSelectValidatesOnlyFinalSelection(t *testing.T) {
	invalidUTF8 := string([]byte{'b', 'a', 'd', 0xff})
	tests := []struct {
		name     string
		include  string
		exclude  string
		children []string
	}{
		{name: "nonmatching unsafe sibling", include: "glob:alpha", children: []string{"alpha", "~cache"}},
		{name: "nonmatching invalid UTF-8 sibling", include: "glob:alpha", children: []string{"alpha", invalidUTF8}},
		{name: "unsafe match removed by exclusion", include: "glob:*", exclude: "glob:~*", children: []string{"alpha", "~cache"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exclude := []Selector(nil)
			if test.exclude != "" {
				exclude = []Selector{mustSelector(t, test.exclude)}
			}
			set, err := NewSkillSet(SkillSetSpec{
				Source:      sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
				Include:     []Selector{mustSelector(t, test.include)},
				Exclude:     exclude,
				Targets:     []target.Target{target.TargetCodex},
				Scope:       target.ScopeProject,
				InstallMode: InstallModeCopy,
			})
			if err != nil {
				t.Fatalf("NewSkillSet returned error: %v", err)
			}

			names, err := set.Select(test.children)
			if err != nil {
				t.Fatalf("Select rejected an unsafe unselected sibling: %v", err)
			}
			if !slices.Equal(names, []string{"alpha"}) {
				t.Fatalf("Select = %#v, want only alpha", names)
			}
		})
	}
}

func TestSkillSetSelectsTreatsUnmatchedChildAsNegativeMembership(t *testing.T) {
	set, err := NewSkillSet(SkillSetSpec{
		Source:      sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
		Include:     []Selector{mustSelector(t, "glob:active"), mustSelector(t, "regex:^other$")},
		Exclude:     []Selector{mustSelector(t, "glob:draft-*")},
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: InstallModeCopy,
	})
	if err != nil {
		t.Fatalf("NewSkillSet returned error: %v", err)
	}

	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "active", want: true},
		{name: "other", want: true},
		{name: "removed", want: false},
		{name: "draft-active", want: false},
	} {
		got, selectErr := set.Selects(test.name)
		if selectErr != nil {
			t.Fatalf("Selects(%q) returned error: %v", test.name, selectErr)
		}
		if got != test.want {
			t.Fatalf("Selects(%q) = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestSkillSetSelectsRejectsUnsafeChildName(t *testing.T) {
	set, err := NewSkillSet(SkillSetSpec{
		Source:      sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
		Include:     []Selector{mustSelector(t, "glob:*")},
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: InstallModeCopy,
	})
	if err != nil {
		t.Fatalf("NewSkillSet returned error: %v", err)
	}

	if _, err := set.Selects("../escape"); err == nil {
		t.Fatal("Selects accepted unsafe child name")
	}
}

func TestSkillSetChildRejectsSourcePolicyDriftFromRoot(t *testing.T) {
	set, err := NewSkillSet(SkillSetSpec{
		Source:       sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
		Include:      []Selector{mustSelector(t, "glob:*")},
		Targets:      []target.Target{target.TargetCodex},
		Scope:        target.ScopeProject,
		InstallMode:  InstallModeCopy,
		Portable:     true,
		CompatRepair: true,
	})
	if err != nil {
		t.Fatalf("NewSkillSet returned error: %v", err)
	}
	if _, err := set.Child("alpha", sourcetest.Local(t, "skills/alpha", source.LocalSourceModeLink)); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("Child error = %v, want source-root relationship rejection", err)
	}
}

func mustSelector(t *testing.T, value string) Selector {
	t.Helper()
	selector, err := ParseSelector(value)
	if err != nil {
		t.Fatalf("ParseSelector(%q) returned error: %v", value, err)
	}
	return selector
}
