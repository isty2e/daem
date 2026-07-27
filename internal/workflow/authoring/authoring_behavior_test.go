package authoring

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
)

func TestSkillAddBehaviorMergesTargetsAndRejectsConflicts(t *testing.T) {
	original := []byte(`version = 1
targets = ["codex", "claude-code"]

[[skill]]
id = "review"
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["codex"]

[[skill]]
name = "inherited"
source = { path = "skills/inherited", mode = "vendor" }
`)

	updated, changeKind, err := ApplyAddSkillToManifest(original, declarationcodec.Skill{
		ID:      "review",
		Name:    "review",
		Source:  declarationcodec.SkillSource{Path: "skills/review", Mode: "vendor"},
		Targets: []string{"claude-code"},
	})
	if err != nil {
		t.Fatalf("ApplyAddSkillToManifest returned error: %v", err)
	}
	if changeKind != "update skill targets" {
		t.Fatalf("change kind = %q, want update skill targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["codex", "claude-code"]`)

	if _, _, err := ApplyAddSkillToManifest(original, declarationcodec.Skill{
		ID:      "review",
		Name:    "other",
		Source:  declarationcodec.SkillSource{Path: "skills/other", Mode: "vendor"},
		Targets: []string{"claude-code"},
	}); err == nil || !strings.Contains(err.Error(), `duplicate skill id "review"`) {
		t.Fatalf("duplicate id err = %v, want duplicate skill id", err)
	}

	if _, _, err := ApplyAddSkillToManifest(original, declarationcodec.Skill{
		Name:    "inherited",
		Source:  declarationcodec.SkillSource{Path: "skills/inherited", Mode: "vendor"},
		Targets: []string{"codex"},
	}); err == nil || !strings.Contains(err.Error(), "inherits manifest targets") {
		t.Fatalf("inherited target err = %v, want inherited target diagnostic", err)
	}
}

func TestSkillRemoveBehaviorHandlesInheritedTargetsAndGroups(t *testing.T) {
	original := []byte(`version = 1
targets = ["codex", "claude-code"]

[[skill]]
name = "inherited"
source = { path = "skills/inherited", mode = "vendor" }

[[skill_group]]
names = ["alpha", "beta"]
source = { path = "skills", mode = "vendor" }
targets = ["codex", "claude-code"]

[[skill_group]]
names = ["solo"]
source = { path = "skills", mode = "vendor" }
targets = ["codex"]
`)

	materialized, changeKind, err := ApplyRemoveSkillToManifest(original, RemoveSkillRequest{
		ResourceKey: "inherited",
		Targets:     []string{"codex"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveSkillToManifest inherited returned error: %v", err)
	}
	if changeKind != "update skill targets" {
		t.Fatalf("change kind = %q, want update skill targets", changeKind)
	}
	requireContains(t, string(materialized), `targets = ["claude-code"]`)

	memberRemoved, changeKind, err := ApplyRemoveSkillToManifest(original, RemoveSkillRequest{
		ResourceKey: "alpha",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveSkillToManifest group member returned error: %v", err)
	}
	if changeKind != "remove skill_group member" {
		t.Fatalf("change kind = %q, want remove skill_group member", changeKind)
	}
	requireContains(t, string(memberRemoved), `names = ["beta"]`)
	requireNotContains(t, string(memberRemoved), `"alpha"`)

	groupRemoved, changeKind, err := ApplyRemoveSkillToManifest(original, RemoveSkillRequest{
		ResourceKey: "solo",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveSkillToManifest empty group returned error: %v", err)
	}
	if changeKind != "remove empty skill_group" {
		t.Fatalf("change kind = %q, want remove empty skill_group", changeKind)
	}
	requireNotContains(t, string(groupRemoved), `names = ["solo"]`)

	if _, _, err := ApplyRemoveSkillToManifest(original, RemoveSkillRequest{
		ResourceKey: "alpha",
		Targets:     []string{"codex"},
	}); err == nil || !strings.Contains(err.Error(), `skill_group member "alpha" shares targets`) {
		t.Fatalf("partial group target err = %v, want partial target diagnostic", err)
	}

	targetedMemberRemoved, changeKind, err := ApplyRemoveSkillToManifest(original, RemoveSkillRequest{
		ResourceKey: "alpha",
		Targets:     []string{"codex", "claude-code"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveSkillToManifest group member with all targets returned error: %v", err)
	}
	if changeKind != "remove skill_group member" {
		t.Fatalf("change kind = %q, want remove skill_group member", changeKind)
	}
	requireContains(t, string(targetedMemberRemoved), `names = ["beta"]`)
	requireNotContains(t, string(targetedMemberRemoved), `"alpha"`)
}

func TestSkillRemoveBehaviorKeepsSelectorBackedGroupNonEditableWithNestedSource(t *testing.T) {
	original := []byte(`version = 1
targets = ["codex"]

[[skill_group]] # user-authored comment
include = ["glob:*"]
targets = ["codex"]

[skill_group.source]
path = "skills"
mode = "vendor"
`)

	_, _, err := ApplyRemoveSkillToManifest(original, RemoveSkillRequest{
		ResourceKey: "alpha",
	})
	if err == nil || !strings.Contains(err.Error(), "selector-backed skill_group children are not edited") {
		t.Fatalf("err = %v, want selector-backed skill_group diagnostic", err)
	}
}

func TestInstructionAddBehaviorNormalizesSourceAndMergesTargets(t *testing.T) {
	original := []byte(`version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	updated, changeKind, err := ApplyAddInstructionToManifest(original, "project", declarationcodec.Instruction{
		Source:  declarationcodec.InstructionSource{Path: "instructions/project.md", Mode: "vendor"},
		Targets: []string{"claude-code"},
	}, declaration.ManifestHeader{Targets: []string{"codex", "claude-code"}})
	if err != nil {
		t.Fatalf("ApplyAddInstructionToManifest returned error: %v", err)
	}
	if changeKind != "update instruction targets" {
		t.Fatalf("change kind = %q, want update instruction targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["codex", "claude-code"]`)
}

func TestCodecOwnedDeclarationTypesRenderBoundaryFields(t *testing.T) {
	original := []byte("version = 1\n")
	portable := false

	skillContent, _, err := ApplyAddSkillToManifest(original, declarationcodec.Skill{
		ID:       "review",
		Name:     "review",
		Source:   declarationcodec.SkillSource{Git: "https://github.com/owner/repo.git", Path: "skills/review", Ref: "main"},
		Targets:  []string{"codex"},
		Scope:    "project",
		Portable: &portable,
	})
	if err != nil {
		t.Fatalf("ApplyAddSkillToManifest returned error: %v", err)
	}
	requireContains(t, string(skillContent), `source = { git = "https://github.com/owner/repo.git", path = "skills/review", ref = "main" }`)
	requireContains(t, string(skillContent), `portable = false`)

	groupContent := ApplyAddSkillGroupToManifest(original, declarationcodec.SkillGroup{
		Names:    []string{"oracle", "review"},
		Source:   declarationcodec.SkillSource{Git: "https://github.com/owner/repo.git", Path: "skills", Ref: "main"},
		Targets:  []string{"codex"},
		Scope:    "global",
		Portable: &portable,
	})
	requireContains(t, string(groupContent), `names = ["oracle", "review"]`)
	requireContains(t, string(groupContent), `source = { git = "https://github.com/owner/repo.git", path = "skills", ref = "main" }`)
	requireContains(t, string(groupContent), `portable = false`)

	hookContent, _, err := ApplyAddHookToManifest(original, declaration.Hook{
		Name:    "lint",
		Event:   "PreToolUse",
		Command: "make lint",
		Targets: []string{"codex"},
		TargetOverrides: []declaration.HookTargetOverride{{
			Target:  "codex",
			Matcher: "Write",
		}},
	}, declaration.ManifestHeader{})
	if err != nil {
		t.Fatalf("ApplyAddHookToManifest returned error: %v", err)
	}
	requireContains(t, string(hookContent), `[[hook.target_override]]`)
	requireContains(t, string(hookContent), `matcher = "Write"`)

	instructionContent, _, err := ApplyAddInstructionToManifest(original, "project", declarationcodec.Instruction{
		Source:  declarationcodec.InstructionSource{S3: "s3://bucket/project.md", VersionID: "v1", Region: "us-east-1", Format: "file"},
		Targets: []string{"codex"},
		Scope:   "project",
	}, declaration.ManifestHeader{})
	if err != nil {
		t.Fatalf("ApplyAddInstructionToManifest returned error: %v", err)
	}
	requireContains(t, string(instructionContent), `source = { s3 = "s3://bucket/project.md", version_id = "v1", region = "us-east-1", format = "file" }`)
}

func TestInstructionCodecOwnedTypeRendersTargetMap(t *testing.T) {
	instruction := declarationcodec.Instruction{
		Source:  declarationcodec.InstructionSource{Path: "instructions/project.md", Mode: "vendor"},
		Targets: []string{"codex"},
		Scope:   "project",
		Target: map[string]declaration.InstructionTarget{
			"codex": {
				RenderTo: "AGENTS.md",
				Mode:     "copy",
			},
		},
	}

	block := declarationcodec.RenderInstructionBlock("project", instruction)
	requireContains(t, block, `[instructions."project".target."codex"]`)
	requireContains(t, block, `render_to = "AGENTS.md"`)
}

func TestCodecDeclarationValuesOwnRequestCollections(t *testing.T) {
	targets := []string{"codex"}
	skill, err := SkillFromAddRequest(AddSkillRequest{
		SourceArg: t.TempDir(),
		Name:      "review",
		Targets:   targets,
	}, t.TempDir())
	if err != nil {
		t.Fatalf("SkillFromAddRequest returned error: %v", err)
	}
	targets[0] = "claude-code"
	if skill.Targets[0] != "codex" {
		t.Fatalf("skill targets = %#v, want owned request snapshot", skill.Targets)
	}

	groupTargets := []string{"codex"}
	group, err := SkillGroupFromAddRequest(AddSkillGroupRequest{
		SourceArg: t.TempDir(),
		Names:     []string{"review"},
		Targets:   groupTargets,
	}, t.TempDir())
	if err != nil {
		t.Fatalf("SkillGroupFromAddRequest returned error: %v", err)
	}
	groupTargets[0] = "claude-code"
	if group.Targets[0] != "codex" {
		t.Fatalf("skill group targets = %#v, want owned request snapshot", group.Targets)
	}

	hookTargets := []string{"codex"}
	overrides := []declaration.HookTargetOverride{{Target: "codex", Matcher: "Write"}}
	hook, _, err := HookFromAddRequest(AddHookRequest{
		Name:            "lint",
		Event:           "PreToolUse",
		Command:         "make lint",
		Targets:         hookTargets,
		TargetOverrides: overrides,
	}, declaration.ManifestHeader{})
	if err != nil {
		t.Fatalf("HookFromAddRequest returned error: %v", err)
	}
	hookTargets[0] = "claude-code"
	overrides[0].Matcher = "Bash"
	if hook.Targets[0] != "codex" || hook.TargetOverrides[0].Matcher != "Write" {
		t.Fatalf("hook = %#v, want owned request snapshot", hook)
	}

	instructionTargets := []string{"codex"}
	instruction, err := InstructionFromAddRequest(AddInstructionRequest{
		Name:      "project",
		SourceArg: "instructions/project.md",
		Targets:   instructionTargets,
	}, t.TempDir(), declaration.ManifestHeader{})
	if err != nil {
		t.Fatalf("InstructionFromAddRequest returned error: %v", err)
	}
	instructionTargets[0] = "claude-code"
	if instruction.Targets[0] != "codex" {
		t.Fatalf("instruction targets = %#v, want owned request snapshot", instruction.Targets)
	}
}

func TestAuthoringRejectsUnknownManifestKeyBeforeHeaderDecode(t *testing.T) {
	_, err := BuildAddHookChange(ManifestDocument{
		Path: "daem.toml",
		Root: t.TempDir(),
		Content: []byte(`version = 1
targets = ["codex"]
unknown = true
`),
	}, AddHookRequest{
		Name:    "protect-env",
		Event:   "PreToolUse",
		Command: "python3 hooks/protect.py",
	})
	if err == nil || !strings.Contains(err.Error(), `invalid manifest: unknown manifest key "unknown"`) {
		t.Fatalf("err = %v, want unknown manifest key validation", err)
	}
}

func TestHookAddBehaviorMergesTargetsAndValidatesOverrides(t *testing.T) {
	original := []byte(`version = 1
targets = ["codex", "claude-code"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
matcher = "Bash"
type = "command"
command = "python3 hooks/protect.py"
targets = ["codex"]
`)

	updated, changeKind, err := ApplyAddHookToManifest(original, declaration.Hook{
		Name:    "protect-env",
		Event:   "PreToolUse",
		Matcher: "Bash",
		Command: "python3 hooks/protect.py",
		Targets: []string{"claude-code"},
	}, declaration.ManifestHeader{Targets: []string{"codex", "claude-code"}})
	if err != nil {
		t.Fatalf("ApplyAddHookToManifest returned error: %v", err)
	}
	if changeKind != "update hook targets" {
		t.Fatalf("change kind = %q, want update hook targets", changeKind)
	}
	requireContains(t, string(updated), `type = "command"`)
	requireContains(t, string(updated), `targets = ["codex", "claude-code"]`)

	if _, _, err := HookFromAddRequest(AddHookRequest{
		Name:    "conditional",
		Event:   "PostToolUse",
		Command: "python3 hooks/protect.py",
		Targets: []string{"codex"},
		TargetOverrides: []declaration.HookTargetOverride{{
			Target:    "codex",
			Condition: "tool_name == 'Write'",
		}},
	}, declaration.ManifestHeader{}); err == nil || !strings.Contains(err.Error(), "target_override.if is not supported") {
		t.Fatalf("HookFromAddRequest err = %v, want unsupported override diagnostic", err)
	}
}

func TestHookAddRejectsAntigravityCLIUnsupportedAuthoring(t *testing.T) {
	tests := []struct {
		name    string
		request AddHookRequest
		header  declaration.ManifestHeader
	}{
		{
			name: "explicit antigravity cli target",
			request: AddHookRequest{
				Name:    "protect-env",
				Event:   "PreToolUse",
				Command: "python3 hooks/protect.py",
				Targets: []string{"antigravity-cli"},
			},
		},
		{
			name: "mixed supported and antigravity cli targets",
			request: AddHookRequest{
				Name:    "protect-env",
				Event:   "PreToolUse",
				Command: "python3 hooks/protect.py",
				Targets: []string{"codex", "antigravity-cli"},
			},
		},
		{
			name: "inherited antigravity cli target",
			request: AddHookRequest{
				Name:    "protect-env",
				Event:   "PreToolUse",
				Command: "python3 hooks/protect.py",
			},
			header: declaration.ManifestHeader{Targets: []string{"antigravity-cli"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, warnings, err := HookFromAddRequest(test.request, test.header)
			if err == nil {
				t.Fatal("HookFromAddRequest returned nil error")
			}
			for _, want := range []string{
				`target "antigravity-cli"`,
				"no direct command-hook route is admitted",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("err = %v, want %q", err, want)
				}
			}
			if len(warnings) != 0 {
				t.Fatalf("warnings = %#v, want none on hard error", warnings)
			}
		})
	}
}

func TestHookAddKeepsOpenCodeBridgeWarning(t *testing.T) {
	_, warnings, err := HookFromAddRequest(AddHookRequest{
		Name:    "protect-env",
		Event:   "PreToolUse",
		Command: "python3 hooks/protect.py",
		Targets: []string{"opencode"},
	}, declaration.ManifestHeader{})
	if err != nil {
		t.Fatalf("HookFromAddRequest returned error: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "hook remains lock-only") {
		t.Fatalf("warnings = %#v, want lock-only bridge warning", warnings)
	}
}

func TestHookRemoveAllowsAntigravityCLIManifestCleanup(t *testing.T) {
	original := []byte(`version = 1
targets = ["codex", "antigravity-cli"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
command = "python3 hooks/protect.py"
targets = ["codex", "antigravity-cli"]
`)

	updated, changeKind, err := ApplyRemoveHookToManifest(original, RemoveHookRequest{
		ResourceName: "protect-env",
		Targets:      []string{"antigravity-cli"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveHookToManifest returned error: %v", err)
	}
	if changeKind != "update hook targets" {
		t.Fatalf("change kind = %q, want update hook targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["codex"]`)
}
