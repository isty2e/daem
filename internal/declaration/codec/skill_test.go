package codec

import (
	"strings"
	"testing"
)

func TestSkillApplyAddMergesTargetsWithoutOwningWorkflowPolicy(t *testing.T) {
	existing := Skill{
		ID: "shared", Name: "lint", Source: SkillSource{Git: "https://example.test/repo.git", Path: "skills/lint", Ref: "main"},
		Targets: []string{"codex"}, Scope: "project",
	}
	incoming := existing
	incoming.Targets = []string{"claude-code"}
	original := []byte("# keep\n" + RenderSkillBlock(existing))

	change, err := ApplySkillAdd(original, incoming)
	if err != nil {
		t.Fatalf("ApplySkillAdd: %v", err)
	}
	got := string(change.Content)
	if !strings.HasPrefix(got, "# keep\n") {
		t.Fatalf("unrelated bytes changed:\n%s", got)
	}
	if !strings.Contains(got, `targets = ["codex", "claude-code"]`) {
		t.Fatalf("merged targets missing:\n%s", got)
	}
}

func TestSkillResourceIDAndIdentityAreDocumentLocal(t *testing.T) {
	left := Skill{ID: " id ", Name: " name ", Scope: " project ", Source: SkillSource{Path: "skill", Mode: "vendor"}}
	right := Skill{ID: "different", Name: "name", Scope: "project", Source: left.Source}
	if got := left.ResourceID(); got != "id" {
		t.Fatalf("ResourceID = %q, want id", got)
	}
	if !sameSkillIdentity(left, right) {
		t.Fatal("declaration ID incorrectly participated in merge identity")
	}
}

func TestSkillScanBlocksFindsSkillTables(t *testing.T) {
	original := []byte(`targets = ["codex", "claude-code"]

[[skill]]
id = "alpha"
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
targets = ["codex", "claude-code"]
scope = "project"

[[skill]]
name = "beta"
source = { path = "skills/beta", mode = "vendor" }
targets = ["codex"]
`)

	blocks, err := ScanSkillBlocks(original)
	if err != nil {
		t.Fatalf("ScanSkillBlocks() error = %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("ScanSkillBlocks() returned %d blocks, want 2", len(blocks))
	}
	if blocks[0].Skill.ID != "alpha" || blocks[1].Skill.Name != "beta" {
		t.Fatalf("blocks = %#v, want alpha and beta skills", blocks)
	}
}

func TestSkillScanBlocksPreservesDuplicateSkillDeclarations(t *testing.T) {
	original := []byte(`[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }

[[skill]]
name = "alpha"
source = { path = "skills/alpha-copy", mode = "vendor" }
`)

	blocks, err := ScanSkillBlocks(original)
	if err != nil {
		t.Fatalf("ScanSkillBlocks() error = %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("ScanSkillBlocks() returned %d blocks, want duplicate blocks preserved", len(blocks))
	}
	if blocks[0].Skill.Source.Path == blocks[1].Skill.Source.Path {
		t.Fatalf("duplicate declarations were collapsed: %#v", blocks)
	}
}

func TestSkillScanBlocksKeepsNestedSourceTableInSkillBlock(t *testing.T) {
	original := []byte(`[[skill]] # user-authored comment
name = "alpha"
targets = ["codex"]

[skill.source]
path = "skills/alpha"
mode = "vendor"

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
`)

	blocks, err := ScanSkillBlocks(original)
	if err != nil {
		t.Fatalf("ScanSkillBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("ScanSkillBlocks() returned %d blocks, want 1", len(blocks))
	}
	if blocks[0].Skill.Source.Path != "skills/alpha" || blocks[0].Skill.Source.Mode != "vendor" {
		t.Fatalf("source = %#v, want nested source table", blocks[0].Skill.Source)
	}
	block := string(original[blocks[0].Start:blocks[0].End])
	requireSkillContains(t, block, `[skill.source]`)
	if strings.Contains(block, `[[mcp_server]]`) {
		t.Fatalf("block = %q, want following mcp_server block excluded", block)
	}
}

func TestSkillScanBlocksEndsSkillBeforeSkillGroupPrefixCollision(t *testing.T) {
	original := []byte(`[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
targets = ["codex"]

[[skill_group]]
names = ["beta"]
source = { path = "skills", mode = "vendor" }
targets = ["codex"]
`)

	blocks, err := ScanSkillBlocks(original)
	if err != nil {
		t.Fatalf("ScanSkillBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("ScanSkillBlocks() returned %d blocks, want 1", len(blocks))
	}
	block := string(original[blocks[0].Start:blocks[0].End])
	requireSkillContains(t, block, `name = "alpha"`)
	if strings.Contains(block, `[[skill_group]]`) {
		t.Fatalf("skill block included skill_group sibling: %q", block)
	}
}

func TestSkillScanBlocksReportsMalformedSkillBlock(t *testing.T) {
	_, err := ScanSkillBlocks([]byte(`[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor"
`))
	if err == nil || !strings.Contains(err.Error(), "parse existing skill block") {
		t.Fatalf("ScanSkillBlocks() error = %v, want parse existing skill block error", err)
	}
}

func TestSkillRenderBlockWritesSkillSyntax(t *testing.T) {
	rendered := RenderSkillBlock(Skill{
		Name:    "gamma",
		Source:  SkillSource{Path: "skills/gamma", Mode: "vendor"},
		Targets: []string{"codex"},
	})

	requireSkillContains(t, rendered, `[[skill]]`)
	requireSkillContains(t, rendered, `source = { path = "skills/gamma", mode = "vendor" }`)
	requireSkillContains(t, rendered, `targets = ["codex"]`)
}

func TestSkillReplaceTargetsPreservesBlockShape(t *testing.T) {
	block := `[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
targets = ["codex", "claude-code"]
`

	updated := ReplaceSkillTargets(block, []string{"claude-code"})

	requireSkillContains(t, updated, `targets = ["claude-code"]`)
	requireSkillContains(t, updated, `name = "alpha"`)
}

func requireSkillContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Fatalf("content = %q, want fragment %q", content, fragment)
	}
}
