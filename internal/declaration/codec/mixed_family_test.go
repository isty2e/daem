package codec

import (
	"strings"
	"testing"
)

func TestFamilyScannersKeepIndependentRangesInMixedCRLFDocument(t *testing.T) {
	content := strings.ReplaceAll(`version = 1

[[skill_group]] # group comment
names = ["alpha", "beta"]
targets = ["codex"]

[skill_group.source]
path = "skills"
mode = "vendor"

[[skill]] # first skill comment
name = "alpha"
targets = ["codex"]

[skill.source]
path = "skills/alpha"
mode = "vendor"

[[skill]]
name = "beta"
source = { path = "skills/beta", mode = "vendor" }
targets = ["claude-code"]

[[hook]] # hook comment
name = "lint"
event = "PreToolUse"
command = "make lint"
targets = ["claude-code"]

[[hook.target_override]]
target = "claude-code"
matcher = "Write"

[instructions."project"] # instruction comment
source = "AGENTS.md"
targets = ["codex"]

[instructions."project".target."codex"]
render_to = "AGENTS.md"

[[mcp_server]] # server comment
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"

[mcp_server.env."API TOKEN"]
from_env = "CONTEXT7_API_TOKEN"

[[extension]] # extension comment
id = "context7-managed"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"

[extension.source]
marketplace = "context7@market"`, "\n", "\r\n")
	original := []byte(content)

	skillGroups, err := ScanSkillGroupBlocks(original)
	if err != nil || len(skillGroups) != 1 {
		t.Fatalf("ScanSkillGroupBlocks() = (%#v, %v), want one block", skillGroups, err)
	}
	skills, err := ScanSkillBlocks(original)
	if err != nil || len(skills) != 2 {
		t.Fatalf("ScanSkillBlocks() = (%#v, %v), want two duplicate-family blocks", skills, err)
	}
	hooks, err := ScanHookBlocks(original)
	if err != nil || len(hooks) != 1 {
		t.Fatalf("ScanHookBlocks() = (%#v, %v), want one block", hooks, err)
	}
	instructions, err := ScanInstructionBlocks(original)
	if err != nil || len(instructions) != 1 {
		t.Fatalf("ScanInstructionBlocks() = (%#v, %v), want one block", instructions, err)
	}
	servers, err := ScanMCPServerBlocks(original)
	if err != nil || len(servers) != 1 {
		t.Fatalf("ScanMCPServerBlocks() = (%#v, %v), want one block", servers, err)
	}
	extensions, err := ScanExtensionBlocks(original)
	if err != nil || len(extensions) != 1 {
		t.Fatalf("ScanExtensionBlocks() = (%#v, %v), want one block", extensions, err)
	}

	if skills[0].Skill.Name != "alpha" || skills[1].Skill.Name != "beta" {
		t.Fatalf("skills = %#v, want ordered alpha and beta declarations", skills)
	}
	if skillGroups[0].Group.Source.Path != "skills" ||
		skills[0].Skill.Source.Path != "skills/alpha" ||
		len(hooks[0].Hook.TargetOverrides) != 1 ||
		instructions[0].Instruction.Target["codex"].RenderTo != "AGENTS.md" ||
		servers[0].Server.Env["API TOKEN"].FromEnv != "CONTEXT7_API_TOKEN" ||
		extensions[0].Extension.Source.Marketplace != "context7@market" {
		t.Fatalf("mixed document lost nested family facts")
	}

	assertMixedFamilyRange(t, original, skillGroups[0].Start, skillGroups[0].End, "# group comment", "[[skill]]")
	assertMixedFamilyRange(t, original, skills[0].Start, skills[0].End, "# first skill comment", "name = \"beta\"")
	assertMixedFamilyRange(t, original, skills[1].Start, skills[1].End, "name = \"beta\"", "[[hook]]")
	assertMixedFamilyRange(t, original, hooks[0].Start, hooks[0].End, "# hook comment", "[instructions.")
	assertMixedFamilyRange(t, original, instructions[0].Start, instructions[0].End, "# instruction comment", "[[mcp_server]]")
	assertMixedFamilyRange(t, original, servers[0].Start, servers[0].End, "# server comment", "[[extension]]")
	assertMixedFamilyRange(t, original, extensions[0].Start, extensions[0].End, "# extension comment", "[[mcp_server]]")
}

func assertMixedFamilyRange(t *testing.T, content []byte, start int, end int, want string, unwanted string) {
	t.Helper()
	block := string(content[start:end])
	if !strings.Contains(block, want) {
		t.Fatalf("block = %q, want %q", block, want)
	}
	if strings.Contains(block, unwanted) {
		t.Fatalf("block = %q, did not want sibling marker %q", block, unwanted)
	}
}
