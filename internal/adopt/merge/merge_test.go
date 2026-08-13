package merge

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/adopt"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

type mergeTestInput struct {
	Merge           bool
	OriginalContent []byte
	Sources         []adopt.Source
	Skills          []adopt.Skill
	Hooks           []adopt.Hook
	MCPServers      []adopt.MCPServer
	SelectorSkills  []desiredskill.Skill
}

func requireContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Fatalf("content = %q, want fragment %q", content, fragment)
	}
}

func TestIntoManifestReportsSameNameInstructionConflictWithoutDroppingPlan(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[instructions.codex_project]
source = "other.md"
targets = ["codex"]
scope = "project"
`),
		Sources: []adopt.Source{{
			ResourceName: "codex_project",
			Target:       target.TargetCodex,
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/instructions/codex-project.md",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want conflict", merged.MergeResults())
	}
	if len(merged.Sources()) != 0 {
		t.Fatalf("sources = %#v, want conflicting source removed from writable additions", merged.Sources())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatalf("manifest content changed on conflict")
	}
}

func TestIntoManifestMergesMissingInstructionTargetThroughDeclarationPatch(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex", "claude-code"]

[instructions.codex_project]
source = "daem.d/instructions/codex-project.md"
targets = ["codex"]
scope = "project"
`),
		Sources: []adopt.Source{{
			ResourceName: "codex_project",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/instructions/codex-project.md",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	if len(merged.Sources()) != 0 {
		t.Fatalf("sources = %#v, want target merge instead of new source", merged.Sources())
	}
	if !strings.Contains(string(merged.ManifestContent()), `targets = ["codex", "claude-code"]`) {
		t.Fatalf("manifest content =\n%s", merged.ManifestContent())
	}
}

func TestIntoManifestReportsInstructionRenderingConflict(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["antigravity-cli"]

[instructions.antigravity_cli_project_gemini]
source = "daem.d/instructions/antigravity-cli-project-gemini.md"
targets = ["antigravity-cli"]
scope = "project"

[instructions.antigravity_cli_project_gemini.target.antigravity-cli]
render_to = "OTHER.md"
`),
		Sources: []adopt.Source{{
			ResourceName: "antigravity_cli_project_gemini",
			Target:       target.TargetAntigravityCLI,
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/instructions/antigravity-cli-project-gemini.md",
			RenderTo:     "GEMINI.md",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want rendering conflict", merged.MergeResults())
	}
	if len(merged.Sources()) != 0 {
		t.Fatalf("sources = %#v, want conflicting source removed from writable additions", merged.Sources())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatalf("manifest content changed on rendering conflict")
	}
	if got := merged.MergeResults()[0].Detail; !strings.Contains(got, "different source, scope, or rendering") {
		t.Fatalf("detail = %q, want rendering conflict detail", got)
	}
}

func TestScanExistingDeclarationsHandlesNestedAndCommentedTables(t *testing.T) {
	content := []byte(`version = 1
targets = ["codex", "claude-code"]

[instructions."project.daily"]
source = "daem.d/instructions/project-daily.md"
targets = ["codex"]

[instructions."project.daily".target."codex"] # user-authored comment
render_to = "AGENTS.md"

[[skill]] # user-authored comment
name = "review"
targets = ["codex"]
scope = "project"

[skill.source]
path = "daem.d/skills/review"
mode = "vendor"

[[hook]]
name = "lint"
event = "PreToolUse"
matcher = "Write"
command = "make lint"
targets = ["codex"]

[[hook.target_override]] # user-authored comment
target = "codex"
matcher = "Edit"
`)

	manifestRoot := t.TempDir()
	existing, err := scanExistingDeclarations(content, manifestRoot, nil, false)
	if err != nil {
		t.Fatalf("scanExistingDeclarations returned error: %v", err)
	}
	if len(existing.Instructions) != 1 || len(existing.Skills) != 1 || len(existing.Hooks) != 1 {
		t.Fatalf("existing = %#v, want one instruction, skill, and hook", existing)
	}
	skillSource, ok := existing.Skills[0].Skill.Source().Local()
	if !ok || skillSource.Path() != "daem.d/skills/review" || !existing.Skills[0].CanMergeTargets {
		t.Fatalf("skill declaration = %#v, want direct nested local source", existing.Skills[0])
	}
	effective, err := existing.Hooks[0].EffectiveMatch(target.TargetCodex)
	if err != nil || effective.Matcher() != "Edit" {
		t.Fatalf("effective Hook match = %#v, %v", effective, err)
	}
}

func TestIntoManifestNoopsCanonicalInstructionDefaultsAndRelativeSource(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[defaults]
scope = "project"

[instructions.codex_project]
source = "daem.d/instructions/codex-project.md"

[instructions.codex_project.target.codex]
mode = "copy"
`),
		Sources: []adopt.Source{{
			ResourceName: "codex_project",
			Target:       target.TargetCodex,
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/instructions/codex-project.md",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %q, want noop; results = %#v", got, merged.MergeResults())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatal("manifest content changed for semantically equivalent instructions")
	}
}

func TestIntoManifestNoopsCanonicalSkillDefaultsAndRelativeSource(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[defaults]
scope = "project"
install_mode = "copy"

[[skill]]
name = "review"
source = { path = "daem.d/skills/review", mode = "vendor" }
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetCodex,
			Targets:      []target.Target{target.TargetCodex},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %q, want noop; results = %#v", got, merged.MergeResults())
	}
}

func TestIntoManifestNoopsCanonicalHookDefaults(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[defaults]
scope = "project"

[[hook]]
name = "lint"
event = "PreToolUse"
command = "make lint"
`),
		Hooks: []adopt.Hook{{
			ResourceName: "lint",
			Target:       target.TargetCodex,
			Scope:        target.ScopeProject,
			Event:        "PreToolUse",
			Command:      "make lint",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %q, want noop; results = %#v", got, merged.MergeResults())
	}
}

func TestIntoManifestRejectsUnrelatedInvalidDeclarationBeforeMerge(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[instructions.codex_project]
source = "daem.d/instructions/codex-project.md"

[[hook]]
name = "invalid"
event = "PreToolUse"
`),
		Sources: []adopt.Source{{
			ResourceName: "codex_project",
			Target:       target.TargetCodex,
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/instructions/codex-project.md",
		}},
	}

	if _, err := mergeTestPlan(t, plan); err == nil || !strings.Contains(err.Error(), "decode merge output manifest") {
		t.Fatalf("merge error = %v, want whole-manifest canonical validation failure", err)
	}
}

func TestIntoManifestNoopsEquivalentExplicitSkillGroupMember(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[defaults]
scope = "project"
install_mode = "copy"

[[skill_group]]
names = ["review"]
source = { path = "daem.d/skills", mode = "vendor" }
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetCodex,
			Targets:      []target.Target{target.TargetCodex},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %q, want noop; results = %#v", got, merged.MergeResults())
	}
	if len(merged.Skills()) != 0 {
		t.Fatalf("skills = %#v, want grouped member retained as non-writable authority", merged.Skills())
	}
}

func TestIntoManifestNoopsEquivalentLockedSelectorSkillGroupMember(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[[skill_group]]
include = ["glob:*"]
source = { path = "daem.d/skills", mode = "vendor" }
targets = ["codex"]
scope = "project"
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetCodex,
			Targets:      []target.Target{target.TargetCodex},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review",
		}},
		SelectorSkills: []desiredskill.Skill{
			selectorBackedMergeSkill(t, "daem.d/skills/review", target.TargetCodex),
		},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %q, want noop; results = %#v", got, merged.MergeResults())
	}
	if len(merged.Skills()) != 0 {
		t.Fatalf("skills = %#v, want selector-backed member retained as non-writable authority", merged.Skills())
	}
}

func TestIntoManifestRequiresLockedSelectorMembershipForSkillMerge(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[[skill_group]]
include = ["glob:*"]
source = { path = "daem.d/skills", mode = "vendor" }
targets = ["codex"]
scope = "project"
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetCodex,
			Targets:      []target.Target{target.TargetCodex},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review",
		}},
	}

	if _, err := mergeTestPlan(t, plan); err == nil ||
		!strings.Contains(err.Error(), "selector-backed skill_group membership is required") {
		t.Fatalf("merge error = %v, want missing membership authority", err)
	}
}

func TestIntoManifestConflictsWithIncompatibleLockedSelectorSkillGroupMember(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[[skill_group]]
include = ["glob:*"]
source = { path = "daem.d/skills", mode = "vendor" }
targets = ["codex"]
scope = "project"
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetCodex,
			Targets:      []target.Target{target.TargetCodex},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/other/review",
		}},
		SelectorSkills: []desiredskill.Skill{
			selectorBackedMergeSkill(t, "daem.d/skills/review", target.TargetCodex),
		},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want selector-backed source conflict", merged.MergeResults())
	}
	if len(merged.Skills()) != 0 {
		t.Fatalf("skills = %#v, want colliding direct declaration suppressed", merged.Skills())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatal("selector-backed skill conflict changed manifest")
	}
}

func TestIntoManifestConflictsOnMemberSpecificSkillGroupTargetMerge(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex", "claude-code"]

[[skill_group]]
names = ["review"]
source = { path = "daem.d/skills", mode = "vendor" }
targets = ["codex"]
scope = "project"
install_mode = "copy"
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetClaudeCode,
			Targets:      []target.Target{target.TargetClaudeCode},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.MergeResults()[0]; got.Status != adopt.MergeStatusConflict ||
		!strings.Contains(got.Detail, "skill_group member") {
		t.Fatalf("merge result = %#v, want grouped member target conflict", got)
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatal("manifest content changed on grouped member conflict")
	}
}

func TestIntoManifestMergesSkillTargetThroughNestedSourceBlock(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex", "claude-code"]

[[skill]] # user-authored comment
name = "review"
targets = ["codex"]
scope = "project"

[skill.source]
path = "daem.d/skills/review"
mode = "vendor"
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetClaudeCode,
			Targets:      []target.Target{target.TargetClaudeCode},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review",
			Placements:   map[target.Target]string{target.TargetClaudeCode: ".claude/skills"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	if len(merged.Skills()) != 0 {
		t.Fatalf("skills = %#v, want target merge instead of new skill", merged.Skills())
	}
	authorities := merged.SkillSourceAuthorities()
	if len(authorities) != 1 || authorities[0].ResourceName != "review" || len(authorities[0].Routes) != 1 {
		t.Fatalf("skill source authorities = %#v, want retained merge-target route", authorities)
	}
	requireContains(t, string(merged.ManifestContent()), `targets = ["codex", "claude-code"]`)
	requireContains(t, string(merged.ManifestContent()), `[skill.source]`)
	requireContains(t, string(merged.ManifestContent()), `[skill.target."claude-code"]`)
	requireContains(t, string(merged.ManifestContent()), `install_to = ".claude/skills"`)
}

func TestIntoManifestRejectsSameTargetAtDifferentSkillPlacement(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["opencode"]

[[skill]]
name = "review"
source = { path = "daem.d/skills/review", mode = "vendor" }
targets = ["opencode"]
scope = "project"
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetOpenCode,
			Targets:      []target.Target{target.TargetOpenCode},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review",
			Placements:   map[target.Target]string{target.TargetOpenCode: ".agents/skills"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want placement conflict", merged.MergeResults())
	}
	if got := merged.MergeResults()[0].Detail; !strings.Contains(got, "placement differs") {
		t.Fatalf("detail = %q, want placement conflict", got)
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatal("manifest content changed on placement conflict")
	}
}

func TestIntoManifestAcceptsSameTargetAtMatchingSkillPlacement(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["opencode"]

[[skill]]
name = "review"
source = { path = "daem.d/skills/review", mode = "vendor" }
targets = ["opencode"]
scope = "project"

[skill.target.opencode]
install_to = ".agents/skills"
`),
		Skills: []adopt.Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       target.TargetOpenCode,
			Targets:      []target.Target{target.TargetOpenCode},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review",
			Placements:   map[target.Target]string{target.TargetOpenCode: ".agents/skills"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want matching placement noop", merged.MergeResults())
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %q, want noop", got)
	}
	if got := len(merged.SkillSourceAuthorities()); got != 1 {
		t.Fatalf("skill source authorities = %d, want retained noop route", got)
	}
}

func TestIntoManifestMergesHookTargetWithoutReencodingExistingBlock(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte("version = 1\r\n" +
			"targets = [\"codex\", \"claude-code\"]\r\n" +
			"\r\n" +
			"[[hook]] # keep header\r\n" +
			"name = 'lint'\r\n" +
			"event = 'PreToolUse'\r\n" +
			"type = 'command'\r\n" +
			"command = 'make lint' # keep command\r\n" +
			"targets = ['codex']\r\n" +
			"scope = 'project'"),
		Hooks: []adopt.Hook{{
			ResourceName: "lint",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Event:        "PreToolUse",
			Command:      "make lint",
			Condition:    "always",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	got := string(merged.ManifestContent())
	for _, want := range []string{
		"[[hook]] # keep header\r\n",
		"command = 'make lint' # keep command\r\n",
		`targets = ["codex", "claude-code"]`,
		`target = "claude-code"`,
		`if = "always"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("content = %q, want %q", got, want)
		}
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("content contains mixed line endings: %q", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("terminal newline was added: %q", got)
	}
}

func TestIntoManifestNoopsTargetEffectiveHookOverride(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex", "claude-code"]

[[hook]]
name = "lint"
event = "PreToolUse"
matcher = "Write"
command = "make lint"
targets = ["codex", "claude-code"]
scope = "project"

[[hook.target_override]]
target = "claude-code"
matcher = "Bash"
if = "always"
`),
		Hooks: []adopt.Hook{{
			ResourceName: "lint",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Event:        "PreToolUse",
			Matcher:      "Bash",
			Command:      "make lint",
			Condition:    "always",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %q, want effective override noop; results = %#v", got, merged.MergeResults())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatal("effective override noop changed manifest")
	}
}

func TestIntoManifestNoopsRedundantHookMatcherOverride(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[[hook]]
name = "lint"
event = "PreToolUse"
matcher = "Write"
command = "make lint"
targets = ["codex"]
scope = "project"

[[hook.target_override]]
target = "codex"
matcher = "Write"
`),
		Hooks: []adopt.Hook{{
			ResourceName: "lint",
			Target:       target.TargetCodex,
			Scope:        target.ScopeProject,
			Event:        "PreToolUse",
			Matcher:      "Write",
			Command:      "make lint",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %q, want redundant override noop; results = %#v", got, merged.MergeResults())
	}
}

func TestIntoManifestConflictsOnDifferentTargetEffectiveHookMatch(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["claude-code"]

[[hook]]
name = "lint"
event = "PreToolUse"
matcher = "Write"
command = "make lint"
targets = ["claude-code"]
scope = "project"

[[hook.target_override]]
target = "claude-code"
matcher = "Bash"
if = "always"
`),
		Hooks: []adopt.Hook{{
			ResourceName: "lint",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Event:        "PreToolUse",
			Matcher:      "Write",
			Command:      "make lint",
			Condition:    "always",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want effective matcher conflict", merged.MergeResults())
	}
	if len(merged.Hooks()) != 0 {
		t.Fatalf("hooks = %#v, want conflicting Hook removed from additions", merged.Hooks())
	}
}

func TestIntoManifestReportsSameDestinationSkillConflict(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[[skill]]
id = "existing"
name = "review"
source = { path = "daem.d/skills/review/sha256-old", mode = "vendor" }
targets = ["codex"]
scope = "project"
`),
		Skills: []adopt.Skill{{
			ResourceName: "incoming",
			InstallName:  "review",
			Target:       target.TargetCodex,
			Targets:      []target.Target{target.TargetCodex},
			Scope:        target.ScopeProject,
			SourcePath:   "daem.d/skills/review/sha256-new",
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want conflict", merged.MergeResults())
	}
	if got := merged.MergeResults()[0].Detail; !strings.Contains(got, "already used by skill id") {
		t.Fatalf("detail = %q", got)
	}
}

func TestIntoManifestAppendsMCPServer(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["claude-code"]
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
			Env:          map[string]string{"TOKEN": "TOKEN"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	if len(merged.MCPServers()) != 1 {
		t.Fatalf("mcp servers = %#v, want appended server", merged.MCPServers())
	}
	requireContains(t, string(merged.ManifestContent()), `[[mcp_server]]`)
	requireContains(t, string(merged.ManifestContent()), `name = "context7"`)
	requireContains(t, string(merged.ManifestContent()), `command = "npx"`)
	requireContains(t, string(merged.ManifestContent()), `[mcp_server.env.TOKEN]`)
	requireContains(t, string(merged.ManifestContent()), `from_env = "TOKEN"`)
}

func TestIntoManifestNoopsEquivalentMCPServer(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
env = { TOKEN = { from_env = "TOKEN" } }
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
			Env:          map[string]string{"TOKEN": "TOKEN"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want no conflict", merged.MergeResults())
	}
	if len(merged.MCPServers()) != 0 {
		t.Fatalf("mcp servers = %#v, want no writable additions", merged.MCPServers())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatalf("manifest changed on noop")
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusNoop {
		t.Fatalf("status = %s, want noop", got)
	}
	if got := merged.MergeResults()[0].Subject.String(); got != "projection/claude-code.project.mcp-server/context7" {
		t.Fatalf("subject = %q, want canonical Claude project projection", got)
	}
}

func TestIntoManifestReportsSameNameMCPServerConflict(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want conflict", merged.MergeResults())
	}
	if len(merged.MCPServers()) != 0 {
		t.Fatalf("mcp servers = %#v, want conflicting server removed from writable additions", merged.MCPServers())
	}
	if string(merged.ManifestContent()) != string(plan.OriginalContent) {
		t.Fatalf("manifest content changed on conflict")
	}
	if got := merged.MergeResults()[0].Detail; !strings.Contains(got, "projection target=claude-code scope=project has a different standalone payload") {
		t.Fatalf("detail = %q", got)
	}
	if got := merged.MergeResults()[0].Subject.String(); got != "projection/claude-code.project.mcp-server/context7" {
		t.Fatalf("subject = %q, want canonical conflicting projection", got)
	}
}

func TestIntoManifestAppendsSameNameMCPAtDifferentProjectionSubject(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetClaudeCode,
			Scope:        target.ScopeProject,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() {
		t.Fatalf("merge results = %#v, want distinct subject addition", merged.MergeResults())
	}
	if len(merged.MCPServers()) != 1 {
		t.Fatalf("mcp servers = %#v, want imported distinct subject", merged.MCPServers())
	}
	if count := strings.Count(string(merged.ManifestContent()), `name = "context7"`); count != 2 {
		t.Fatalf("manifest content = %q, want two same-name MCP rows", merged.ManifestContent())
	}
	if got := merged.MergeResults()[0].Status; got != adopt.MergeStatusAdd {
		t.Fatalf("status = %q, want add", got)
	}
	if got := merged.MergeResults()[0].Subject.String(); got != "projection/claude-code.project.mcp-server/context7" {
		t.Fatalf("subject = %q, want canonical added projection", got)
	}
}

func TestIntoManifestRejectsGlobalMCPThatInheritsScopeFromDefaults(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[defaults]
scope = "global"

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetCodex,
			Scope:        target.ScopeGlobal,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
		}},
	}

	_, err := mergeTestPlan(t, plan)
	if err == nil || !strings.Contains(err.Error(), "global MCP projection requires explicit scope") {
		t.Fatalf("merge error = %v, want explicit global scope rejection", err)
	}
}

func TestIntoManifestNoopsMCPThatInheritsProjectScopeFromDefaults(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[defaults]
scope = "project"

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetCodex,
			Scope:        target.ScopeProject,
			Command:      "npx",
			Args:         []string{"-y", "@upstash/context7-mcp"},
		}},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() || len(merged.MCPServers()) != 0 {
		t.Fatalf("merge = %#v, want inherited project-scope noop", merged)
	}
	result := merged.MergeResults()[0]
	if result.Status != adopt.MergeStatusNoop ||
		result.Subject.String() != "projection/codex.project.mcp-server/context7" {
		t.Fatalf("merge result = %#v, want canonical project-scope noop", result)
	}
}

func TestIntoManifestNoopsExistingScopeAndAddsMissingScopeForSameMCPName(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["project-server"]
`),
		MCPServers: []adopt.MCPServer{
			{
				ResourceName: "context7",
				Target:       target.TargetCodex,
				Scope:        target.ScopeProject,
				Command:      "npx",
				Args:         []string{"project-server"},
			},
			{
				ResourceName: "context7",
				Target:       target.TargetCodex,
				Scope:        target.ScopeGlobal,
				Command:      "uvx",
				Args:         []string{"global-server==1.2.3"},
			},
		},
	}

	merged, err := mergeTestPlan(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if merged.HasMergeConflicts() || len(merged.MCPServers()) != 1 {
		t.Fatalf("merge results = %#v candidates = %#v, want one noop and one add", merged.MergeResults(), merged.MCPServers())
	}
	results := merged.MergeResults()
	if len(results) != 2 || results[0].Status != adopt.MergeStatusNoop || results[1].Status != adopt.MergeStatusAdd {
		t.Fatalf("merge results = %#v, want project noop then global add", results)
	}
	if authorities := merged.MCPSourceAuthorities(); len(authorities) != 2 {
		t.Fatalf("MCP source authorities = %#v, want noop and add routes", authorities)
	}
	wantSubjects := []string{
		"projection/codex.project.mcp-server/context7",
		"projection/codex.global.mcp-server/context7",
	}
	for index, result := range results {
		if got := result.Subject.String(); got != wantSubjects[index] {
			t.Fatalf("merge result %d subject = %q, want %q", index, got, wantSubjects[index])
		}
	}
	content := string(merged.ManifestContent())
	if strings.Count(content, `name = "context7"`) != 2 || !strings.Contains(content, `scope = "global"`) {
		t.Fatalf("manifest = %q, want project and global same-name rows", content)
	}
}

func TestIntoManifestRejectsDuplicateExistingMCPProjectionSubject(t *testing.T) {
	plan := mergeTestInput{
		Merge: true,
		OriginalContent: []byte(`version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"
`),
		MCPServers: []adopt.MCPServer{{
			ResourceName: "context7",
			Target:       target.TargetCodex,
			Scope:        target.ScopeProject,
			Command:      "npx",
		}},
	}

	_, err := mergeTestPlan(t, plan)
	if err == nil || !strings.Contains(err.Error(), "duplicate") ||
		!strings.Contains(err.Error(), "target=codex scope=project") {
		t.Fatalf("merge error = %v, want duplicate existing subject rejection", err)
	}
}

func mergeTestPlan(t *testing.T, input mergeTestInput) (adopt.Plan, error) {
	t.Helper()
	if !input.Merge {
		t.Fatal("merge test input must select merge")
	}
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := adopt.NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := adopt.NewRequest(
		profile.ImportableTargets(),
		[]target.Scope{target.ScopeProject, target.ScopeGlobal},
		output,
		sourceDirectory,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}

	for index := range input.Sources {
		if !filepath.IsAbs(input.Sources[index].SourcePath) {
			input.Sources[index].SourcePath = filepath.Join(root, filepath.FromSlash(input.Sources[index].SourcePath))
		}
		if input.Sources[index].LivePath == "" {
			input.Sources[index].LivePath = filepath.Join(root, "live-instructions")
		}
		if len(input.Sources[index].Content) == 0 {
			input.Sources[index].Content = []byte("imported instructions\n")
		}
	}
	for index := range input.Skills {
		if !filepath.IsAbs(input.Skills[index].SourcePath) {
			input.Skills[index].SourcePath = filepath.Join(root, filepath.FromSlash(input.Skills[index].SourcePath))
		}
		if input.Skills[index].GroupRoot != "" && !filepath.IsAbs(input.Skills[index].GroupRoot) {
			input.Skills[index].GroupRoot = filepath.Join(root, filepath.FromSlash(input.Skills[index].GroupRoot))
		}
		if len(input.Skills[index].Targets) == 0 {
			input.Skills[index].Targets = []target.Target{input.Skills[index].Target}
		}
		if len(input.Skills[index].SourceRoutes) == 0 {
			livePath := filepath.Join(root, "live-skill")
			input.Skills[index].SourceRoutes = []adopt.SkillSourceRoute{{
				Target: input.Skills[index].Target, LivePath: livePath, ReadPath: livePath,
			}}
		}
		if input.Skills[index].ContentHash == "" {
			input.Skills[index].ContentHash = artifact.HashFileContent([]byte("merge test skill"))
		}
	}
	for index := range input.Hooks {
		if input.Hooks[index].LivePath == "" {
			input.Hooks[index].LivePath = filepath.Join(root, "live-hooks")
		}
	}
	for index := range input.MCPServers {
		if input.MCPServers[index].SourceRoute.PrimaryPath == "" {
			primaryPath := filepath.Join(root, "live-mcp")
			route, routeErr := adopt.NewMCPSourceRoute(adopt.MCPSourceRouteInput{
				PrimaryPath:     primaryPath,
				PrimaryRevision: "merge-test-source-revision",
				MaximumBytes:    1024,
				ContentPath:     "/mcp/" + input.MCPServers[index].ResourceName,
			})
			if routeErr != nil {
				t.Fatal(routeErr)
			}
			input.MCPServers[index].SourceRoute = route
		}
	}
	candidates, err := adopt.NewCandidateSet(adopt.CandidateSetInput{
		Sources:    input.Sources,
		Skills:     input.Skills,
		Hooks:      input.Hooks,
		MCPServers: input.MCPServers,
	})
	if err != nil {
		return adopt.Plan{}, err
	}
	return IntoManifest(request, input.OriginalContent, candidates, input.SelectorSkills)
}

func selectorBackedMergeSkill(
	t *testing.T,
	path string,
	targets ...target.Target,
) desiredskill.Skill {
	t.Helper()
	skillSource, err := sourcepkg.NewLocalSource(path, sourcepkg.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredskill.New(desiredskill.Spec{
		Name:        "review",
		InstallName: "review",
		Source:      skillSource,
		Targets:     targets,
		Scope:       target.ScopeProject,
		InstallMode: desiredskill.InstallModeCopy,
		Portable:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
