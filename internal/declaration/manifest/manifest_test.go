package manifest

import (
	"path/filepath"
	"testing"

	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	desiredinstructions "github.com/isty2e/daem/internal/desired/instructions"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func TestParseNormalizesManifest(t *testing.T) {
	environment, err := Decode([]byte(`
version = 1
targets = ["codex", "claude-code"]

[defaults]
scope = "project"
install_mode = "copy"

[instructions.project]
source = "AGENTS.md"
targets = ["codex", "claude-code"]

[instructions.project.target.claude-code]
render_to = " CLAUDE.md "
mode = "symlink"

[[skill]]
name = "oracle"
source = { git = "https://github.com/steipete/oracle.git", path = "skills/oracle", ref = "main" }
scope = "global"

[[skill]]
name = "local-review"
source = { path = "skills/local-review", mode = "vendor" }
targets = ["codex"]

[[hook]]
name = "bd-prime-session"
event = "SessionStart"
matcher = "startup|resume"
type = "command"
command = "bd prime"
timeout = 30

[[hook.target_override]]
target = "claude-code"
matcher = "startup"
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}

	if len(environment.Targets()) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(environment.Targets()))
	}

	skills := environment.Skills()
	if len(skills) != 2 {
		t.Fatalf("len(Skills) = %d, want 2", len(skills))
	}

	oracle := skills[0]
	if oracle.ID().Name() != "oracle" {
		t.Fatalf("first skill name = %q, want oracle", oracle.ID().Name())
	}
	if oracle.InstallName() != "oracle" {
		t.Fatalf("first skill install name = %q, want oracle", oracle.InstallName())
	}

	gitSource, ok := oracle.Source().Git()
	if !ok {
		t.Fatal("first skill source is not git")
	}

	if gitSource.RepositoryPath().String() != "skills/oracle" || gitSource.Ref().String() != "main" {
		t.Fatalf("git source = %#v, want path skills/oracle at main", gitSource)
	}

	localSource, ok := skills[1].Source().Local()
	if !ok {
		t.Fatal("second skill source is not local")
	}
	if skills[1].Scope() != target.ScopeProject || skills[1].InstallMode() != desiredskill.InstallModeCopy {
		t.Fatalf("second skill defaults = %s/%s, want project/copy", skills[1].Scope(), skills[1].InstallMode())
	}

	if localSource.Mode() != source.LocalSourceModeVendor {
		t.Fatalf("local source mode = %q, want vendor", localSource.Mode())
	}

	hooks := environment.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("len(Hooks) = %d, want 1", len(hooks))
	}

	hook := hooks[0]
	if hook.Type() != desiredhook.TypeCommand {
		t.Fatalf("hook.Type = %q, want command", hook.Type())
	}

	effective, err := hook.EffectiveMatch(target.TargetClaudeCode)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Matcher() != "startup" {
		t.Fatalf("effective matcher = %q, want startup", effective.Matcher())
	}

	instructionValues := environment.Instructions()
	if len(instructionValues) != 1 {
		t.Fatalf("len(Instructions) = %d, want 1", len(instructionValues))
	}

	instruction := instructionValues[0]
	instructionSource, ok := instruction.Source().Local()
	if !ok {
		t.Fatal("instruction source is not local")
	}
	if instructionSource.Path() != "AGENTS.md" || instructionSource.Mode() != source.LocalSourceModeVendor {
		t.Fatalf("instruction source = %#v, want AGENTS.md vendor", instructionSource)
	}
	rendering, ok := instruction.Renderings()[target.TargetClaudeCode]
	if !ok {
		t.Fatal("missing claude-code instruction rendering")
	}

	if rendering.RenderTo() != "CLAUDE.md" || rendering.Mode() != desiredinstructions.RenderModeSymlink {
		t.Fatalf("claude rendering = %#v, want CLAUDE.md symlink", rendering)
	}
}

func TestParseSkillIDSeparatesResourceIDFromSkillName(t *testing.T) {
	sourcePath := filepath.ToSlash(filepath.Join(t.TempDir(), "skills", "review"))
	environment, err := Decode([]byte(`
	version = 1
	targets = ["codex"]

	[[skill]]
	id = "codex_global_review"
	name = "review"
	source = { path = "` + sourcePath + `", mode = "vendor" }
	targets = ["codex"]
	scope = "global"
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	skills := environment.Skills()
	if len(skills) != 1 {
		t.Fatalf("skills = %#v", skills)
	}
	skill := skills[0]
	if skill.ID().Name() != "codex_global_review" {
		t.Fatalf("ID.Name = %q, want codex_global_review", skill.ID().Name())
	}
	if skill.InstallName() != "review" {
		t.Fatalf("InstallName = %q, want review", skill.InstallName())
	}
}

func TestParseAcceptsAntigravityCLIOnlyAsCurrentAntigravityTarget(t *testing.T) {
	environment, err := Decode([]byte(`
version = 1
targets = ["antigravity-cli"]

[[skill]]
name = "antigravity-guide"
source = { path = "skills/antigravity-guide", mode = "vendor" }
targets = ["antigravity-cli"]
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	skills := environment.Skills()
	if len(skills) != 1 {
		t.Fatalf("skills = %#v, want one skill", skills)
	}
	if len(skills[0].Targets()) != 1 || skills[0].Targets()[0] != target.TargetAntigravityCLI {
		t.Fatalf("skill targets = %#v, want antigravity-cli", skills[0].Targets())
	}

	for _, targetValue := range []string{"antigravity", "antigravity-ide"} {
		t.Run(targetValue, func(t *testing.T) {
			_, err := Decode([]byte(`
version = 1
targets = ["` + targetValue + `"]
`))
			if err == nil {
				t.Fatal("Bytes returned nil error")
			}
		})
	}
}

func TestParseNormalizesSkillCompatRepairPolicy(t *testing.T) {
	environment, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "repairable"
source = { path = "skills/repairable", mode = "vendor" }
compat_repair = true

[[skill]]
name = "manual"
source = { path = "skills/manual", mode = "vendor" }
compat_repair = false

[[skill]]
name = "omitted"
source = { path = "skills/omitted", mode = "vendor" }
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	skills := environment.Skills()
	if len(skills) != 3 {
		t.Fatalf("len(Skills) = %d, want 3", len(skills))
	}
	if !skills[0].CompatRepair() {
		t.Fatalf("first skill CompatRepair = false, want true")
	}
	if skills[1].CompatRepair() {
		t.Fatalf("second skill CompatRepair = true, want false")
	}
	if skills[2].CompatRepair() {
		t.Fatalf("third skill CompatRepair = true, want omitted false")
	}
}

func TestParseNormalizesSelectorBackedSkillGroup(t *testing.T) {
	environment, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:review-*", "regex:^oracle$"]
exclude = ["glob:review-draft"]
targets = ["codex"]
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if len(environment.Skills()) != 0 {
		t.Fatalf("Skills = %#v, want none before lock-time selector expansion", environment.Skills())
	}
	sets := environment.SkillSets()
	if len(sets) != 1 {
		t.Fatalf("SkillSets = %#v, want one selector-backed set", sets)
	}
	set := sets[0]
	if len(set.Include()) != 2 || set.Include()[0].Kind() != desiredskill.SelectorGlob || set.Include()[1].Kind() != desiredskill.SelectorRegex {
		t.Fatalf("Include selectors = %#v", set.Include())
	}
	if len(set.Exclude()) != 1 || set.Exclude()[0].Expression() != "glob:review-draft" {
		t.Fatalf("Exclude selectors = %#v", set.Exclude())
	}
	if set.Scope() != target.ScopeProject || set.InstallMode() != desiredskill.InstallModeCopy {
		t.Fatalf("set scope/install = %s/%s", set.Scope(), set.InstallMode())
	}
}

func TestParseNormalizesSkillGroupCompatRepairPolicy(t *testing.T) {
	environment, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill_group]]
names = ["alpha", "beta"]
source = { path = "skills", mode = "vendor" }
compat_repair = true

[[skill_group]]
names = ["gamma"]
source = { path = "other-skills", mode = "vendor" }
compat_repair = false

[[skill_group]]
include = ["glob:review-*"]
source = { path = "selector-skills", mode = "vendor" }
compat_repair = true
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	skills := environment.Skills()
	if len(skills) != 3 {
		t.Fatalf("len(Skills) = %d, want 3 explicit group skills", len(skills))
	}
	for index := range 2 {
		if !skills[index].CompatRepair() {
			t.Fatalf("skill %d CompatRepair = false, want true", index)
		}
	}
	if skills[2].CompatRepair() {
		t.Fatalf("third explicit group skill CompatRepair = true, want false")
	}
	sets := environment.SkillSets()
	if len(sets) != 1 {
		t.Fatalf("len(SkillSets) = %d, want one selector-backed set", len(sets))
	}
	if !sets[0].CompatRepair() {
		t.Fatalf("selector-backed group CompatRepair = false, want true")
	}
}
