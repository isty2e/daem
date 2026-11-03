package declaration

import (
	"testing"
)

func TestDeclarationKeysAreDistinctFromResourceKinds(t *testing.T) {
	skill := Skill{ID: "review", Name: "Review"}
	if got := skill.Key(); got != (Key{Kind: KindSkill, Name: "review"}) {
		t.Fatalf("skill key = %#v", got)
	}

	hook := Hook{Name: "lint"}
	if got := hook.Key(); got != (Key{Kind: KindHook, Name: "lint"}) {
		t.Fatalf("hook key = %#v", got)
	}
}

func TestSkillGroupMemberKeysOnlyExistForExplicitNames(t *testing.T) {
	explicit := SkillGroup{Names: []string{"alpha", "beta"}}
	keys := explicit.MemberKeys()
	if len(keys) != 2 || keys[0].Name != "alpha" || keys[1].Name != "beta" {
		t.Fatalf("member keys = %#v, want alpha and beta", keys)
	}

	selectorBacked := SkillGroup{Include: []string{"glob:*"}}
	if keys := selectorBacked.MemberKeys(); len(keys) != 0 {
		t.Fatalf("selector-backed member keys = %#v, want none before intent expansion", keys)
	}
}

func TestTargetValuesReturnsCopy(t *testing.T) {
	skill := Skill{Targets: []string{"codex"}}
	skillValues := skill.TargetValues()
	skillValues[0] = "mutated"
	if skill.Targets[0] != "codex" {
		t.Fatalf("TargetValues mutated skill targets: %#v", skill.Targets)
	}

	hostConfig := HostConfig{Targets: []string{"codex"}}
	hostConfigValues := hostConfig.TargetValues()
	hostConfigValues[0] = "mutated"
	if hostConfig.Targets[0] != "codex" {
		t.Fatalf("TargetValues mutated host_config target values: %#v", hostConfig.Targets)
	}
}
