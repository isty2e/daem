package codec

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestAddSatisfactionUsesEffectiveScopeAndTargetCoverage(t *testing.T) {
	header := declaration.ManifestHeader{Targets: []string{"codex", "claude-code"}, Defaults: declaration.Defaults{Scope: "global"}}
	prefix := "# retain header\nversion = 1\ntargets = [\"codex\", \"claude-code\"]\n[defaults]\nscope = \"global\"\n\n"
	families := []struct {
		name   string
		render func([]string) string
		add    func([]byte, []string, string) (declaration.EditResult, error)
	}{
		{
			name: "skill",
			render: func(targets []string) string {
				return RenderSkillBlock(Skill{Name: "review", Source: SkillSource{Path: "/skills/review", Mode: "vendor"}, Targets: targets})
			},
			add: func(original []byte, targets []string, scope string) (declaration.EditResult, error) {
				return ApplySkillAdd(original, Skill{Name: "review", Source: SkillSource{Path: "/skills/review", Mode: "vendor"}, Targets: targets, Scope: scope})
			},
		},
		{
			name: "instruction",
			render: func(targets []string) string {
				return RenderInstructionBlock("guide", Instruction{Source: InstructionSource{Path: "/guide.md"}, Targets: targets})
			},
			add: func(original []byte, targets []string, scope string) (declaration.EditResult, error) {
				return ApplyInstructionAdd(original, header, "guide", Instruction{Source: InstructionSource{Path: "/guide.md"}, Targets: targets, Scope: scope})
			},
		},
		{
			name: "hook",
			render: func(targets []string) string {
				return RenderHookBlock(declaration.Hook{Name: "lint", Event: "PreToolUse", Command: "true", Targets: targets})
			},
			add: func(original []byte, targets []string, scope string) (declaration.EditResult, error) {
				return ApplyHookAdd(original, header, declaration.Hook{Name: "lint", Event: "PreToolUse", Command: "true", Targets: targets, Scope: scope}, func(existing, incoming declaration.Hook, merged []string, _ declaration.ManifestHeader) (declaration.Hook, error) {
					existing.Targets = merged
					return existing, nil
				})
			},
		},
		{
			name: "skill_group",
			render: func(targets []string) string {
				return RenderSkillGroupBlock(SkillGroup{Names: []string{"oracle", "review"}, Source: SkillSource{Path: "/skills", Mode: "vendor"}, Targets: targets})
			},
			add: func(original []byte, targets []string, scope string) (declaration.EditResult, error) {
				return ApplySkillGroupAdd(original, SkillGroup{Names: []string{"review", "oracle"}, Source: SkillSource{Path: "/skills", Mode: "vendor"}, Targets: targets, Scope: scope})
			},
		},
	}
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			for _, existingTargets := range [][]string{nil, {"codex", "claude-code"}} {
				original := []byte(prefix + family.render(existingTargets) + "# retain trailing comment\n")
				for _, targets := range [][]string{nil, {"codex"}, {"claude-code", "codex"}} {
					for _, scope := range []string{"", "global"} {
						result, err := family.add(original, targets, scope)
						if err != nil || result.Outcome != declaration.EditOutcomeUnchanged || !bytes.Equal(result.Content, original) {
							t.Fatalf("existing=%v incoming=%v scope=%q: result=%#v error=%v", existingTargets, targets, scope, result, err)
						}
					}
				}
				if _, err := family.add(original, []string{"codex"}, "project"); err == nil {
					t.Fatal("different effective scope was accepted")
				}
			}
			original := []byte(prefix + family.render([]string{"codex"}))
			for _, targets := range [][]string{nil, {"claude-code"}} {
				result, err := family.add(original, targets, "global")
				if err != nil || result.Outcome != declaration.EditOutcomeMergeTargets || bytes.Equal(result.Content, original) {
					t.Fatalf("actual target addition=%v: %#v, %v", targets, result, err)
				}
			}
			inherited := []byte(prefix + family.render(nil))
			if _, err := family.add(inherited, []string{"pi"}, "global"); err == nil {
				t.Fatal("inherited membership was silently broadened")
			}
		})
	}
}

func TestSatisfiedHookChecksRequestedOverrides(t *testing.T) {
	existing := declaration.Hook{Name: "lint", Event: "PreToolUse", Command: "true", Targets: []string{"claude-code"}, TargetOverrides: []declaration.HookTargetOverride{{Target: "claude-code", Matcher: "Write"}}}
	original := []byte(RenderHookBlock(existing))
	merge := func(existing, incoming declaration.Hook, targets []string, _ declaration.ManifestHeader) (declaration.Hook, error) {
		t.Fatal("a covered target set must not invoke the target merge renderer")
		return declaration.Hook{}, nil
	}
	result, err := ApplyHookAdd(original, declaration.ManifestHeader{}, existing, merge)
	if err != nil || result.Outcome != declaration.EditOutcomeUnchanged || !bytes.Equal(result.Content, original) {
		t.Fatalf("identical override: %#v, %v", result, err)
	}
	existing.TargetOverrides = []declaration.HookTargetOverride{{Target: "claude-code", Matcher: "Read"}}
	if _, err := ApplyHookAdd(original, declaration.ManifestHeader{}, existing, merge); err == nil {
		t.Fatal("conflicting override was treated as unchanged")
	}
}

func TestCoveredSkillPlacementCannotBecomeAnUnchangedAdd(t *testing.T) {
	original := []byte(RenderSkillBlock(Skill{Name: "review", Source: SkillSource{Path: "skills/review", Mode: "vendor"}, Targets: []string{"codex"}}))
	incoming := Skill{Name: "review", Source: SkillSource{Path: "skills/review", Mode: "vendor"}, Targets: []string{"codex"}, Target: map[string]declaration.SkillTarget{"codex": {InstallTo: "another/review"}}}
	if _, err := ApplySkillAdd(original, incoming); err == nil {
		t.Fatal("requested placement change was silently ignored")
	}
	group := SkillGroup{Names: []string{"review"}, Source: SkillSource{Path: "skills", Mode: "vendor"}, Targets: []string{"codex"}}
	original = []byte(RenderSkillGroupBlock(group))
	group.Target = incoming.Target
	if _, err := ApplySkillGroupAdd(original, group); err == nil {
		t.Fatal("requested group placement change was silently ignored")
	}
}

func TestNamedGroupEditingRejectsSelectorInput(t *testing.T) {
	for _, group := range []SkillGroup{{}, {Include: []string{"*"}}, {Names: []string{"review"}, Exclude: []string{"other"}}} {
		if _, err := ApplySkillGroupAdd([]byte("version = 1\n"), group); err == nil {
			t.Fatalf("non-named input accepted: %#v", group)
		}
	}
}
