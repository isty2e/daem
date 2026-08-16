package authoring

import (
	"strings"
	"testing"

	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
)

func TestSkillAuthoringTargetRemoval(t *testing.T) {
	original := []byte(`targets = ["codex", "claude-code"]

[[skill]]
id = "alpha"
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
targets = ["codex", "claude-code"]
scope = "project"

[skill.target.codex]
install_to = ".agents/skills"

[skill.target.claude-code]
install_to = ".claude/skills"

[[skill]]
name = "beta"
source = { path = "skills/beta", mode = "vendor" }
targets = ["codex"]
`)

	updated, changeKind, err := ApplyRemoveSkillToManifest(original, RemoveSkillRequest{
		ResourceKey: "alpha",
		Targets:     []string{"codex"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveSkillToManifest() error = %v", err)
	}
	if changeKind != "update skill targets" {
		t.Fatalf("change kind = %q, want update skill targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["claude-code"]`)
	requireNotContains(t, string(updated), `[skill.target.codex]`)
	requireContains(t, string(updated), `[skill.target.claude-code]`)
	requireContains(t, string(updated), `name = "beta"`)

	removed, changeKind, err := ApplyRemoveSkillToManifest(updated, RemoveSkillRequest{
		ResourceKey: "alpha",
		Targets:     []string{"claude-code"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveSkillToManifest(second) error = %v", err)
	}
	if changeKind != "remove skill resource" {
		t.Fatalf("change kind = %q, want remove skill resource", changeKind)
	}
	requireNotContains(t, string(removed), `id = "alpha"`)
	requireContains(t, string(removed), `name = "beta"`)
}

func TestSkillAuthoringAddMergePreservesNestedSourceAndFollowingResource(t *testing.T) {
	original := []byte(`[[skill]] # user-authored comment
name = "alpha"
targets = ["codex"]

[skill.source]
path = "skills/alpha"
mode = "vendor"

[[hook]]
name = "later"
event = "PreToolUse"
matcher = "Write"
command = "make fmt"
targets = ["codex"]
`)

	updated, changeKind, err := ApplyAddSkillToManifest(original, declarationcodec.Skill{
		Name:    "alpha",
		Source:  declarationcodec.SkillSource{Path: "skills/alpha", Mode: "vendor"},
		Targets: []string{"claude-code"},
	})
	if err != nil {
		t.Fatalf("ApplyAddSkillToManifest() error = %v", err)
	}
	if changeKind != "update skill targets" {
		t.Fatalf("change kind = %q, want update skill targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["codex", "claude-code"]`)
	requireContains(t, string(updated), `[skill.source]`)
	requireContains(t, string(updated), `[[hook]]`)
	if strings.Count(string(updated), `[[skill]]`) != 1 {
		t.Fatalf("updated = %q, want one skill block", updated)
	}
}

func TestInstructionAuthoringTargetTableRemoval(t *testing.T) {
	original := []byte(`[instructions."project"]
source = "AGENTS.md"
targets = ["codex", "claude-code"]
scope = "project"

[instructions."project".target."codex"]
render_to = "AGENTS.md"

[instructions."project".target."claude-code"]
render_to = "CLAUDE.md"

[instructions."other"]
source = "OTHER.md"
targets = ["codex"]
`)

	updated, changeKind, err := ApplyRemoveInstructionToManifest(original, RemoveInstructionRequest{
		ResourceName: "project",
		Targets:      []string{"codex"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveInstructionToManifest() error = %v", err)
	}
	if changeKind != "update instruction targets" {
		t.Fatalf("change kind = %q, want update instruction targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["claude-code"]`)
	requireContains(t, string(updated), `[instructions."project".target."claude-code"]`)
	requireNotContains(t, string(updated), `[instructions."project".target."codex"]`)
}

func TestInstructionAuthoringTargetRemovalHandlesQuotedNamesAndFollowingMCP(t *testing.T) {
	original := []byte(`[instructions."project.daily"]
source = "AGENTS.md"
targets = ["codex", "claude-code"]

[instructions."project.daily".target."codex"] # user-authored comment
render_to = "AGENTS.md"

[instructions."project.daily".target."claude-code"]
render_to = "CLAUDE.md"

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"`)

	updated, changeKind, err := ApplyRemoveInstructionToManifest(original, RemoveInstructionRequest{
		ResourceName: "project.daily",
		Targets:      []string{"codex"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveInstructionToManifest() error = %v", err)
	}
	if changeKind != "update instruction targets" {
		t.Fatalf("change kind = %q, want update instruction targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["claude-code"]`)
	requireContains(t, string(updated), `[instructions."project.daily".target."claude-code"]`)
	requireContains(t, string(updated), `[[mcp_server]]`)
	requireNotContains(t, string(updated), `[instructions."project.daily".target."codex"]`)
}

func TestInstructionAuthoringRemovalPreservesFollowingResourceBlocks(t *testing.T) {
	original := []byte(`[instructions."project"]
source = "AGENTS.md"
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
matcher = "Bash"
command = "python3 hooks/protect.py"
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
`)

	updated, changeKind, err := ApplyRemoveInstructionToManifest(original, RemoveInstructionRequest{
		ResourceName: "project",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveInstructionToManifest() error = %v", err)
	}
	if changeKind != "remove instruction resource" {
		t.Fatalf("change kind = %q, want remove instruction resource", changeKind)
	}
	requireNotContains(t, string(updated), `[instructions."project"]`)
	requireContains(t, string(updated), `[[skill]]`)
	requireContains(t, string(updated), `name = "oracle"`)
	requireContains(t, string(updated), `[[hook]]`)
	requireContains(t, string(updated), `name = "protect-env"`)
	requireContains(t, string(updated), `[[mcp_server]]`)
	requireContains(t, string(updated), `name = "context7"`)
}

func TestHookAuthoringTargetOverrideRemoval(t *testing.T) {
	original := []byte(`[[hook]]
name = "lint"
event = "PreToolUse"
matcher = "Write"
command = "make lint"
targets = ["codex", "claude-code"]
scope = "project"

[[hook.target_override]]
target = "codex"
matcher = "Edit"
`)

	updated, changeKind, err := ApplyRemoveHookToManifest(original, RemoveHookRequest{
		ResourceName: "lint",
		Targets:      []string{"codex"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveHookToManifest() error = %v", err)
	}
	if changeKind != "update hook targets" {
		t.Fatalf("change kind = %q, want update hook targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["claude-code"]`)
	requireNotContains(t, string(updated), `[[hook.target_override]]`)
}

func TestHookAuthoringTargetOverrideRemovalPreservesFollowingSkill(t *testing.T) {
	original := []byte(`[[hook]]
name = "lint"
event = "PreToolUse"
matcher = "Write"
command = "make lint"
targets = ["codex", "claude-code"]
scope = "project"

[[hook.target_override]] # user-authored comment
target = "codex"
matcher = "Edit"

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]`)

	updated, changeKind, err := ApplyRemoveHookToManifest(original, RemoveHookRequest{
		ResourceName: "lint",
		Targets:      []string{"codex"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveHookToManifest() error = %v", err)
	}
	if changeKind != "update hook targets" {
		t.Fatalf("change kind = %q, want update hook targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["claude-code"]`)
	requireContains(t, string(updated), `[[skill]]`)
	requireContains(t, string(updated), `name = "oracle"`)
	requireNotContains(t, string(updated), `[[hook.target_override]]`)
}

func TestHookAuthoringPartialRemovalMaterializesInheritedTargets(t *testing.T) {
	original := []byte(`version = 1
targets = ["codex", "claude-code"]

[[hook]]
name = "lint"
event = "PreToolUse"
command = "make lint"
scope = "project"
`)

	updated, changeKind, err := ApplyRemoveHookToManifest(original, RemoveHookRequest{
		ResourceName: "lint",
		Targets:      []string{"claude-code"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveHookToManifest() error = %v", err)
	}
	if changeKind != "update hook targets" {
		t.Fatalf("change kind = %q, want update hook targets", changeKind)
	}
	requireContains(t, string(updated), `targets = ["codex"]`)
	requireContains(t, string(updated), `name = "lint"`)
	hookBlock := string(updated)[strings.Index(string(updated), "[[hook]]"):]
	if !strings.Contains(hookBlock, `targets = ["codex"]`) || strings.Contains(hookBlock, `targets = ["codex", "claude-code"]`) {
		t.Fatalf("inherited hook targets were not materialized:\n%s", hookBlock)
	}
}

func TestHookAuthoringPartialRemovalRestoresInheritanceWhenRemainingMatchesHeader(t *testing.T) {
	original := []byte(`version = 1
targets = ["codex"]

[[hook]]
name = "lint"
event = "PreToolUse"
command = "make lint"
targets = ["codex", "claude-code"]
scope = "project"
`)

	updated, changeKind, err := ApplyRemoveHookToManifest(original, RemoveHookRequest{
		ResourceName: "lint",
		Targets:      []string{"claude-code"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveHookToManifest() error = %v", err)
	}
	if changeKind != "update hook targets" {
		t.Fatalf("change kind = %q, want update hook targets", changeKind)
	}
	requireContains(t, string(updated), `name = "lint"`)
	if strings.Count(string(updated), "targets =") != 1 {
		t.Fatalf("expected only manifest header targets to remain:\n%s", updated)
	}
	requireContains(t, string(updated), `targets = ["codex"]`)
	requireNotContains(t, string(updated), `targets = ["codex", "claude-code"]`)
}

func TestSkillGroupAuthoringMemberRemovalPreservesNestedSourceAndFollowingResources(t *testing.T) {
	original := []byte(`[[skill_group]] # user-authored comment
names = ["alpha", "beta"]
targets = ["codex"]
scope = "project"

[skill_group.source]
path = "skills"
mode = "vendor"

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
`)

	updated, changeKind, err := ApplyRemoveSkillToManifest(original, RemoveSkillRequest{
		ResourceKey: "alpha",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveSkillToManifest() error = %v", err)
	}
	if changeKind != "remove skill_group member" {
		t.Fatalf("change kind = %q, want remove skill_group member", changeKind)
	}
	requireContains(t, string(updated), `names = ["beta"]`)
	requireContains(t, string(updated), `[skill_group.source]`)
	requireContains(t, string(updated), `path = "skills"`)
	requireContains(t, string(updated), `[[mcp_server]]`)
	requireContains(t, string(updated), `name = "context7"`)
	requireNotContains(t, string(updated), `"alpha"`)
}

func requireContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Fatalf("content = %q, want fragment %q", content, fragment)
	}
}

func requireNotContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if strings.Contains(content, fragment) {
		t.Fatalf("content = %q, want no fragment %q", content, fragment)
	}
}
