package codec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestSkillGroupScanBlocksFindsSkillGroupTables(t *testing.T) {
	original := []byte(`[[skill_group]]
names = ["alpha", "beta"]
source = { path = "skills", mode = "vendor" }
targets = ["codex"]
`)

	blocks, err := ScanSkillGroupBlocks(original)
	if err != nil {
		t.Fatalf("ScanSkillGroupBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("ScanSkillGroupBlocks() returned %d blocks, want 1", len(blocks))
	}
	if got := strings.Join(blocks[0].Group.Names, ","); got != "alpha,beta" {
		t.Fatalf("names = %q, want alpha,beta", got)
	}
}

func TestSkillGroupScanBlocksPreservesSelectorBackedSkillGroupFields(t *testing.T) {
	original := []byte(`[[skill_group]]
include = ["glob:*"]
exclude = ["glob:draft-*"]
source = { path = "skills", mode = "vendor" }
targets = ["codex"]
`)

	blocks, err := ScanSkillGroupBlocks(original)
	if err != nil {
		t.Fatalf("ScanSkillGroupBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("ScanSkillGroupBlocks() returned %d blocks, want 1", len(blocks))
	}
	if got := strings.Join(blocks[0].Group.Include, ","); got != "glob:*" {
		t.Fatalf("include = %q, want glob:*", got)
	}
	if got := strings.Join(blocks[0].Group.Exclude, ","); got != "glob:draft-*" {
		t.Fatalf("exclude = %q, want glob:draft-*", got)
	}
}

func TestSkillGroupScanBlocksKeepsNestedSourceTableInSkillGroupBlock(t *testing.T) {
	original := []byte(`[[skill_group]] # user-authored comment
names = ["alpha", "beta"]
targets = ["codex"]

[skill_group.source]
path = "skills"
mode = "vendor"

[[skill]]
name = "later"
source = { path = "skills/later", mode = "vendor" }
`)

	blocks, err := ScanSkillGroupBlocks(original)
	if err != nil {
		t.Fatalf("ScanSkillGroupBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("ScanSkillGroupBlocks() returned %d blocks, want 1", len(blocks))
	}
	if blocks[0].Group.Source.Path != "skills" || blocks[0].Group.Source.Mode != "vendor" {
		t.Fatalf("source = %#v, want nested source table", blocks[0].Group.Source)
	}
	block := string(original[blocks[0].Start:blocks[0].End])
	requireSkillGroupContains(t, block, `[skill_group.source]`)
	if strings.Contains(block, `[[skill]]`) {
		t.Fatalf("block = %q, want following skill block excluded", block)
	}
}

func TestSkillGroupScanBlocksDoesNotTreatHeaderLikeAssignmentAsBoundary(t *testing.T) {
	original := []byte(`[[skill_group]]
names = ["alpha"]
source = { path = "skills", mode = "vendor" }
targets = ["codex"]
note = "[[skill]] trailing"

[[hook]]
name = "later"
`)

	blocks, err := ScanSkillGroupBlocks(original)
	if err != nil {
		t.Fatalf("ScanSkillGroupBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("ScanSkillGroupBlocks() returned %d blocks, want 1", len(blocks))
	}
	block := string(original[blocks[0].Start:blocks[0].End])
	requireSkillGroupContains(t, block, `note = "[[skill]] trailing"`)
	if strings.Contains(block, `[[hook]]`) {
		t.Fatalf("skill_group block included following hook: %q", block)
	}
}

func TestSkillGroupRenderBlockWritesSkillGroupSyntax(t *testing.T) {
	rendered := RenderSkillGroupBlock(SkillGroup{
		Names:   []string{"oracle", "review"},
		Source:  SkillSource{Path: "skills", Mode: "vendor"},
		Targets: []string{"codex"},
	})

	requireSkillGroupContains(t, rendered, `[[skill_group]]`)
	requireSkillGroupContains(t, rendered, `names = ["oracle", "review"]`)
	requireSkillGroupContains(t, rendered, `source = { path = "skills", mode = "vendor" }`)
}

func TestSkillGroupRenderBlockWritesSelectorBackedSkillGroupSyntax(t *testing.T) {
	rendered := RenderSkillGroupBlock(SkillGroup{
		Include: []string{"glob:*"},
		Exclude: []string{"glob:draft-*"},
		Source:  SkillSource{Path: "skills", Mode: "vendor"},
		Targets: []string{"codex"},
	})

	requireSkillGroupContains(t, rendered, `[[skill_group]]`)
	requireSkillGroupContains(t, rendered, `include = ["glob:*"]`)
	requireSkillGroupContains(t, rendered, `exclude = ["glob:draft-*"]`)
	requireSkillGroupContains(t, rendered, `source = { path = "skills", mode = "vendor" }`)
}

func TestSkillGroupReplaceNamesPreservesSkillGroupBlock(t *testing.T) {
	block := `[[skill_group]]
names = ["alpha", "beta"]
source = { path = "skills", mode = "vendor" }
`

	updated := ReplaceSkillGroupNames(block, []string{"beta"})

	requireSkillGroupContains(t, updated, `names = ["beta"]`)
	requireSkillGroupContains(t, updated, `source = { path = "skills", mode = "vendor" }`)
}

func TestSkillGroupReplaceTargetsInsertsBeforeTargetLocalTable(t *testing.T) {
	block := `[[skill_group]]
names = ["alpha"]
source = { path = "skills", mode = "vendor" }

[skill_group.target.codex]
install_to = ".agents/skills"
`

	updated := ReplaceSkillGroupTargets(block, []string{"codex"})

	targetsIndex := strings.Index(updated, `targets = ["codex"]`)
	targetTableIndex := strings.Index(updated, `[skill_group.target.codex]`)
	if targetsIndex < 0 || targetTableIndex < 0 || targetsIndex > targetTableIndex {
		t.Fatalf("root targets were not inserted before target-local table:\n%s", updated)
	}
}

func TestSkillGroupTargetPlacementTableRendersAndScans(t *testing.T) {
	rendered := RenderSkillGroupBlock(SkillGroup{
		Names:   []string{"alpha"},
		Source:  SkillSource{Path: "skills", Mode: "vendor"},
		Targets: []string{"opencode"},
		Scope:   "project",
		Target: map[string]declaration.SkillTarget{
			"opencode": {InstallTo: ".agents/skills"},
		},
	})
	requireSkillGroupContains(t, rendered, `[skill_group.target."opencode"]`)
	requireSkillGroupContains(t, rendered, `install_to = ".agents/skills"`)

	blocks, err := ScanSkillGroupBlocks([]byte(rendered))
	if err != nil || len(blocks) != 1 {
		t.Fatalf("ScanSkillGroupBlocks = %#v, %v", blocks, err)
	}
	if got := blocks[0].Group.Target["opencode"].InstallTo; got != ".agents/skills" {
		t.Fatalf("scanned install_to = %q", got)
	}
}

func TestSkillGroupMembershipUsesDeclarationIndicesAndLaterDuplicates(t *testing.T) {
	content := []byte(`[[skill_group]]
names = ["alpha", "shared"]
source = { path = "first", mode = "vendor" }
targets = ["codex"]

[[skill_group]]
names = ["beta", "shared"]
source = { path = "second", mode = "vendor" }
targets = ["codex"]
`)

	membership, err := SkillGroupMembership(content)
	if err != nil {
		t.Fatalf("SkillGroupMembership() error = %v", err)
	}
	for name, expected := range map[string]string{
		"alpha":  "skill_group[0]",
		"beta":   "skill_group[1]",
		"shared": "skill_group[1]",
	} {
		if actual := membership[name]; actual != expected {
			t.Fatalf("membership[%q] = %q, want %q", name, actual, expected)
		}
	}
}

func requireSkillGroupContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Fatalf("content = %q, want fragment %q", content, fragment)
	}
}
