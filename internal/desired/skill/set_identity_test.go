package skill

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestSkillSetDeclarationIdentityUsesSemanticSelectorAndTargetSets(t *testing.T) {
	base := baseIdentitySkillSetSpec(t)
	baseIdentity := declarationIdentityFor(t, base)

	reordered := cloneSkillSetSpec(base)
	reordered.Include[0], reordered.Include[1] = reordered.Include[1], reordered.Include[0]
	reordered.Targets[0], reordered.Targets[1] = reordered.Targets[1], reordered.Targets[0]
	if got := declarationIdentityFor(t, reordered); !baseIdentity.Equal(got) {
		t.Fatalf("semantic reorder changed identity: %q != %q", baseIdentity.String(), got.String())
	}

	duplicated := cloneSkillSetSpec(base)
	duplicated.Include = append(duplicated.Include, duplicated.Include[0])
	if got := declarationIdentityFor(t, duplicated); !baseIdentity.Equal(got) {
		t.Fatalf("semantically redundant selector changed identity: %q != %q", baseIdentity.String(), got.String())
	}

	withExcludes := cloneSkillSetSpec(base)
	withExcludes.Exclude = append(withExcludes.Exclude, mustSelector(t, "glob:legacy-*"))
	withExcludeIdentity := declarationIdentityFor(t, withExcludes)
	reorderedExcludes := cloneSkillSetSpec(withExcludes)
	reorderedExcludes.Exclude[0], reorderedExcludes.Exclude[1] = reorderedExcludes.Exclude[1], reorderedExcludes.Exclude[0]
	reorderedExcludes.Exclude = append(reorderedExcludes.Exclude, reorderedExcludes.Exclude[0])
	if got := declarationIdentityFor(t, reorderedExcludes); !withExcludeIdentity.Equal(got) {
		t.Fatalf("semantic exclude reorder changed identity: %q != %q", withExcludeIdentity.String(), got.String())
	}

	if !strings.HasPrefix(baseIdentity.String(), "skill-set-declaration:v1:sha256:") {
		t.Fatalf("identity = %q", baseIdentity.String())
	}
	parsed, err := ParseSkillSetDeclarationIdentity(baseIdentity.String())
	if err != nil || !parsed.Equal(baseIdentity) {
		t.Fatalf("ParseSkillSetDeclarationIdentity = %q, %v", parsed.String(), err)
	}
	if err := (SkillSetDeclarationIdentity{}).Validate(); err == nil || (SkillSetDeclarationIdentity{}).String() != "" {
		t.Fatal("zero declaration identity was admitted")
	}
}

func TestSkillSetDeclarationIdentityFramesSetMembers(t *testing.T) {
	left := baseIdentitySkillSetSpec(t)
	left.Include = []Selector{mustSelector(t, "glob:a"), mustSelector(t, "glob:bc")}
	right := cloneSkillSetSpec(left)
	right.Include = []Selector{mustSelector(t, "glob:ab"), mustSelector(t, "glob:c")}

	leftIdentity := declarationIdentityFor(t, left)
	rightIdentity := declarationIdentityFor(t, right)
	if leftIdentity.Equal(rightIdentity) {
		t.Fatalf("length-ambiguous selector sets collided: %q", leftIdentity.String())
	}
}

func TestParseSkillSetDeclarationIdentityRejectsNonCanonicalValues(t *testing.T) {
	valid := declarationIdentityFor(t, baseIdentitySkillSetSpec(t)).String()
	for _, value := range []string{
		"",
		" " + valid,
		valid + " ",
		strings.Replace(valid, ":v1:", ":v2:", 1),
		valid[:len(valid)-1],
		valid + "0",
		strings.ToUpper(valid),
		valid[:len(valid)-1] + "g",
	} {
		if parsed, err := ParseSkillSetDeclarationIdentity(value); err == nil || parsed.String() != "" {
			t.Fatalf("ParseSkillSetDeclarationIdentity(%q) = %q, %v", value, parsed.String(), err)
		}
	}
}

func TestSkillSetDeclarationIdentityCoversEveryCurrentDeclarationAxis(t *testing.T) {
	base := baseIdentitySkillSetSpec(t)
	baseIdentity := declarationIdentityFor(t, base)

	tests := []struct {
		name string
		edit func(*SkillSetSpec)
	}{
		{name: "source locator", edit: func(spec *SkillSetSpec) {
			spec.Source = mustIdentityGitSource(t, "https://github.com/other/skills.git", "skills", "main")
		}},
		{name: "source path", edit: func(spec *SkillSetSpec) {
			spec.Source = mustIdentityGitSource(t, "https://github.com/example/skills.git", "other", "main")
		}},
		{name: "source ref", edit: func(spec *SkillSetSpec) {
			spec.Source = mustIdentityGitSource(t, "https://github.com/example/skills.git", "skills", "next")
		}},
		{name: "source kind", edit: func(spec *SkillSetSpec) {
			spec.Source = sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
		}},
		{name: "include set", edit: func(spec *SkillSetSpec) {
			spec.Include[0] = mustSelector(t, "glob:review-*")
		}},
		{name: "exclude set", edit: func(spec *SkillSetSpec) {
			spec.Exclude = append(spec.Exclude, mustSelector(t, "glob:legacy-*"))
		}},
		{name: "target set", edit: func(spec *SkillSetSpec) {
			spec.Targets = []target.Target{target.TargetCodex}
		}},
		{name: "scope", edit: func(spec *SkillSetSpec) { spec.Scope = target.ScopeGlobal }},
		{name: "install mode", edit: func(spec *SkillSetSpec) { spec.InstallMode = InstallModeSymlink }},
		{name: "repair policy", edit: func(spec *SkillSetSpec) { spec.CompatRepair = !spec.CompatRepair }},
		{name: "portability", edit: func(spec *SkillSetSpec) { spec.Portable = !spec.Portable }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := cloneSkillSetSpec(base)
			test.edit(&changed)
			changedIdentity := declarationIdentityFor(t, changed)
			if baseIdentity.Equal(changedIdentity) {
				t.Fatalf("declaration axis %q did not change identity %q", test.name, baseIdentity.String())
			}
		})
	}
}

func baseIdentitySkillSetSpec(t *testing.T) SkillSetSpec {
	t.Helper()
	return SkillSetSpec{
		Source: mustIdentityGitSource(t, "https://github.com/example/skills.git", "skills", "main"),
		Include: []Selector{
			mustSelector(t, "glob:*"),
			mustSelector(t, "regex:^review"),
		},
		Exclude:      []Selector{mustSelector(t, "glob:draft-*")},
		Targets:      []target.Target{target.TargetCodex, target.TargetClaudeCode},
		Scope:        target.ScopeProject,
		InstallMode:  InstallModeCopy,
		Portable:     true,
		CompatRepair: true,
	}
}

func cloneSkillSetSpec(spec SkillSetSpec) SkillSetSpec {
	clone := spec
	clone.Include = append([]Selector(nil), spec.Include...)
	clone.Exclude = append([]Selector(nil), spec.Exclude...)
	clone.Targets = append([]target.Target(nil), spec.Targets...)
	return clone
}

func declarationIdentityFor(t *testing.T, spec SkillSetSpec) SkillSetDeclarationIdentity {
	t.Helper()
	set, err := NewSkillSet(spec)
	if err != nil {
		t.Fatalf("NewSkillSet returned error: %v", err)
	}
	identity, err := set.DeclarationIdentity()
	if err != nil {
		t.Fatalf("DeclarationIdentity returned error: %v", err)
	}
	if err := identity.Validate(); err != nil {
		t.Fatalf("identity.Validate returned error: %v", err)
	}
	return identity
}

func mustIdentityGitSource(t *testing.T, locator string, repositoryPath string, ref string) source.Source {
	t.Helper()
	value, err := source.NewGitSource(locator, repositoryPath, ref)
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}
	return value
}
