package adopt

import (
	"reflect"
	"testing"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestImportManifestSkillGroupsUseSetIdentityAndProductTargetOrder(t *testing.T) {
	skills := []Skill{
		{
			InstallName: "alpha",
			Targets:     []targetpkg.Target{targetpkg.TargetPi, targetpkg.TargetCodex},
			Scope:       targetpkg.ScopeGlobal,
			GroupRoot:   "skills/group",
		},
		{
			InstallName: "beta",
			Targets:     []targetpkg.Target{targetpkg.TargetCodex, targetpkg.TargetPi},
			Scope:       targetpkg.ScopeGlobal,
			GroupRoot:   "skills/group",
		},
	}

	body, _, err := importManifestTables(nil, skills, nil, nil, nil, make(map[targetpkg.Target]struct{}))
	if err != nil {
		t.Fatalf("importManifestTables returned error: %v", err)
	}
	if len(body.SkillGroups) != 1 {
		t.Fatalf("skill groups = %#v, want one order-independent group", body.SkillGroups)
	}
	group := body.SkillGroups[0]
	if !reflect.DeepEqual(group.Names, []string{"alpha", "beta"}) {
		t.Fatalf("group names = %#v", group.Names)
	}
	if !reflect.DeepEqual(group.Targets, []string{"codex", "pi"}) {
		t.Fatalf("group targets = %#v, want product order", group.Targets)
	}
}

func TestImportManifestDirectSkillAndMergePreserveAuthoredOrder(t *testing.T) {
	body, _, err := importManifestTables(nil, []Skill{{
		ResourceName: "alpha",
		InstallName:  "alpha",
		Targets:      []targetpkg.Target{targetpkg.TargetPi, targetpkg.TargetCodex},
		Scope:        targetpkg.ScopeGlobal,
		SourcePath:   "skills/alpha",
	}}, nil, nil, nil, make(map[targetpkg.Target]struct{}))
	if err != nil {
		t.Fatalf("importManifestTables returned error: %v", err)
	}
	if len(body.Skills) != 1 || !reflect.DeepEqual(body.Skills[0].Targets, []string{"pi", "codex"}) {
		t.Fatalf("direct skill targets = %#v, want authored order", body.Skills)
	}

	merged := mergeImportTargetStrings(
		[]string{"pi", "pi", "codex"},
		[]targetpkg.Target{targetpkg.TargetClaudeCode, targetpkg.TargetPi},
	)
	if !reflect.DeepEqual(merged, []string{"pi", "codex", "claude-code"}) {
		t.Fatalf("merged targets = %#v, want existing-first unique order", merged)
	}
}
