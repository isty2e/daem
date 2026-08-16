package codec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
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

func TestSkillTargetMergePreservesUnsupportedAuthoringFieldsAndCRLF(t *testing.T) {
	original := []byte("[[skill]]\r\n" +
		"id = \"shared\"\r\n" +
		"name = \"lint\"\r\n" +
		"source = { s3 = \"s3://bucket/skill.zip\", version_id = \"v1\", region = \"us-east-1\", format = \"archive\" }\r\n" +
		"targets = [\"codex\"]\r\n" +
		"scope = \"project\"\r\n" +
		"install_mode = \"copy\"\r\n" +
		"compat_repair = true")
	incoming := Skill{
		ID: "shared", Name: "lint",
		Source:  SkillSource{},
		Targets: []string{"claude-code"},
		Scope:   "project",
	}

	blocks, err := ScanSkillBlocks(original)
	if err != nil {
		t.Fatalf("ScanSkillBlocks: %v", err)
	}
	incoming.Source = blocks[0].Skill.Source
	change, err := ApplySkillAdd(original, incoming)
	if err != nil {
		t.Fatalf("ApplySkillAdd: %v", err)
	}
	got := string(change.Content)
	for _, want := range []string{
		`s3 = "s3://bucket/skill.zip"`,
		`version_id = "v1"`,
		`compat_repair = true`,
		`install_mode = "copy"`,
		`targets = ["codex", "claude-code"]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("content = %q, want %q preserved", got, want)
		}
	}
	if strings.Count(got, "\r\n") != 7 || strings.HasSuffix(got, "\n") {
		t.Fatalf("line endings changed: %q", got)
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

	updated, err := ReplaceSkillTargets(block, []string{"claude-code"})
	if err != nil {
		t.Fatalf("ReplaceSkillTargets() error = %v", err)
	}

	requireSkillContains(t, updated, `targets = ["claude-code"]`)
	requireSkillContains(t, updated, `name = "alpha"`)
}

func TestSkillReplaceTargetsRewritesCompactAssignment(t *testing.T) {
	block := `[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
targets=["codex", "claude-code"]
`

	updated, err := ReplaceSkillTargets(block, []string{"claude-code"})
	if err != nil {
		t.Fatalf("ReplaceSkillTargets() error = %v", err)
	}
	requireSkillContains(t, updated, `targets = ["claude-code"]`)
	if strings.Contains(updated, `targets=["codex"`) {
		t.Fatalf("compact assignment left in place:\n%s", updated)
	}
}

func TestSkillReplaceTargetsInsertsBeforeTargetLocalTable(t *testing.T) {
	block := `[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }

[skill.target.codex]
install_to = ".agents/skills"
`

	updated, err := ReplaceSkillTargets(block, []string{"codex"})
	if err != nil {
		t.Fatalf("ReplaceSkillTargets() error = %v", err)
	}

	targetsIndex := strings.Index(updated, `targets = ["codex"]`)
	targetTableIndex := strings.Index(updated, `[skill.target.codex]`)
	if targetsIndex < 0 || targetTableIndex < 0 || targetsIndex > targetTableIndex {
		t.Fatalf("root targets were not inserted before target-local table:\n%s", updated)
	}
}

func TestSkillTargetPlacementTablesRenderScanMergeAndRemoveDeterministically(t *testing.T) {
	existing := Skill{
		Name:    "review",
		Source:  SkillSource{Path: "skills/review", Mode: "vendor"},
		Targets: []string{"opencode"},
		Scope:   "project",
		Target: map[string]declaration.SkillTarget{
			"opencode": {InstallTo: ".opencode/skills"},
		},
	}
	rendered := RenderSkillBlock(existing)
	if strings.Index(rendered, "targets =") > strings.Index(rendered, "[skill.target.") {
		t.Fatalf("root targets rendered inside target table:\n%s", rendered)
	}

	blocks, err := ScanSkillBlocks([]byte(rendered))
	if err != nil || len(blocks) != 1 {
		t.Fatalf("ScanSkillBlocks = %#v, %v", blocks, err)
	}
	if got := blocks[0].Skill.Target["opencode"].InstallTo; got != ".opencode/skills" {
		t.Fatalf("scanned install_to = %q", got)
	}

	incoming := existing
	incoming.Targets = []string{"codex"}
	incoming.Target = map[string]declaration.SkillTarget{
		"codex": {InstallTo: ".agents/skills"},
	}
	updated, err := UpdateSkillTargets(rendered, existing, incoming, []string{"opencode", "codex"})
	if err != nil {
		t.Fatalf("UpdateSkillTargets returned error: %v", err)
	}
	for _, fragment := range []string{
		`[skill.target."opencode"]`,
		`install_to = ".opencode/skills"`,
		`[skill.target."codex"]`,
		`install_to = ".agents/skills"`,
	} {
		requireSkillContains(t, updated, fragment)
	}

	removed := RemoveSkillTargetTables(updated, "skill", []string{"opencode"})
	requireSkillContains(t, removed, `[skill.target."codex"]`)
	if strings.Contains(removed, `[skill.target."opencode"]`) {
		t.Fatalf("selected target table remains:\n%s", removed)
	}
}

func TestSkillTargetPlacementMergeRejectsConflictingMetadata(t *testing.T) {
	existing := Skill{
		ID: "review", Name: "review", Source: SkillSource{Path: "skills/review", Mode: "vendor"},
		Targets: []string{"codex"}, Scope: "project",
		Target: map[string]declaration.SkillTarget{"codex": {InstallTo: ".agents/skills"}},
	}
	incoming := existing
	incoming.Target = map[string]declaration.SkillTarget{"codex": {InstallTo: ".codex/skills"}}
	if _, err := UpdateSkillTargets(RenderSkillBlock(existing), existing, incoming, existing.Targets); err == nil {
		t.Fatal("UpdateSkillTargets accepted conflicting target metadata")
	}
}

func requireSkillContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Fatalf("content = %q, want fragment %q", content, fragment)
	}
}
