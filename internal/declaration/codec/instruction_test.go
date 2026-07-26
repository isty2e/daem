package codec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestInstructionApplyAddMergesInstructionTargetsAndPreservesNestedTables(t *testing.T) {
	header := declaration.ManifestHeader{}
	existing := Instruction{
		Source:  InstructionSource{Path: "AGENTS.md", Mode: "vendor"},
		Targets: []string{"codex"}, Scope: "project",
		Target: map[string]InstructionRendering{"codex": {RenderTo: "AGENTS.md", Mode: "copy"}},
	}
	incoming := existing
	incoming.Targets = []string{"claude-code"}
	original := []byte("# keep\n" + RenderInstructionBlock("project", existing))

	change, err := ApplyInstructionAdd(original, header, "project", incoming)
	if err != nil {
		t.Fatalf("ApplyInstructionAdd: %v", err)
	}
	got := string(change.Content)
	if !strings.HasPrefix(got, "# keep\n") {
		t.Fatalf("unrelated bytes changed:\n%s", got)
	}
	if !strings.Contains(got, `targets = ["codex", "claude-code"]`) {
		t.Fatalf("merged targets missing:\n%s", got)
	}
	if !strings.Contains(got, `[instructions."project".target."codex"]`) {
		t.Fatalf("nested target table was lost:\n%s", got)
	}
}

func TestInstructionEffectiveScopeUsesReservedInstructionNameBeforeHeaderDefault(t *testing.T) {
	header := declaration.ManifestHeader{}
	header.Defaults.Scope = "global"
	if got := InstructionEffectiveScope("project", "", header); got != "project" {
		t.Fatalf("InstructionEffectiveScope(project) = %q", got)
	}
	if got := InstructionEffectiveScope("custom", "", header); got != "global" {
		t.Fatalf("InstructionEffectiveScope(custom) = %q", got)
	}
	if got := InstructionEffectiveScope("custom", "", declaration.ManifestHeader{}); got != "project" {
		t.Fatalf("InstructionEffectiveScope(custom, zero header) = %q", got)
	}
}

func TestInstructionScanBlocksFindsInstructionTables(t *testing.T) {
	original := []byte(`[instructions."project"]
source = "AGENTS.md"
targets = ["codex", "claude-code"]
scope = "project"

[instructions."project".target."codex"] # user-authored comment
render_to = "AGENTS.md"

[instructions."project".target."claude-code"]
render_to = "CLAUDE.md"

[instructions."other"]
source = "OTHER.md"
targets = ["codex"]
`)

	blocks, err := ScanInstructionBlocks(original)
	if err != nil {
		t.Fatalf("ScanInstructionBlocks() error = %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("ScanInstructionBlocks() returned %d blocks, want 2", len(blocks))
	}
	if blocks[0].Name != "project" || blocks[1].Name != "other" {
		t.Fatalf("blocks = %#v, want project and other", blocks)
	}
}

func TestInstructionScanBlocksEndsInstructionBeforeArrayTable(t *testing.T) {
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
`)

	blocks, err := ScanInstructionBlocks(original)
	if err != nil {
		t.Fatalf("ScanInstructionBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("ScanInstructionBlocks() returned %d blocks, want 1", len(blocks))
	}
	block := string(original[blocks[0].Start:blocks[0].End])
	requireInstructionContains(t, block, `[instructions."project"]`)
	requireInstructionNotContains(t, block, `[[skill]]`)
	requireInstructionNotContains(t, block, `[[hook]]`)
}

func TestInstructionScanBlocksEndsCRLFInstructionBeforeArrayTableWithoutTrailingNewline(t *testing.T) {
	original := []byte("[instructions.\"project\"]\r\n" +
		"source = \"AGENTS.md\"\r\n" +
		"targets = [\"codex\"]\r\n" +
		"\r\n" +
		"[[skill]]\r\n" +
		"name = \"oracle\"\r\n" +
		"source = { path = \"skills/oracle\", mode = \"vendor\" }")

	blocks, err := ScanInstructionBlocks(original)
	if err != nil {
		t.Fatalf("ScanInstructionBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("ScanInstructionBlocks() returned %d blocks, want 1", len(blocks))
	}
	block := string(original[blocks[0].Start:blocks[0].End])
	requireInstructionContains(t, block, `[instructions."project"]`)
	requireInstructionNotContains(t, block, `[[skill]]`)
}

func TestInstructionScanBlocksReportsMalformedInstructionBlock(t *testing.T) {
	_, err := ScanInstructionBlocks([]byte(`[instructions."project"]
source = { path = "AGENTS.md", mode = "vendor"
`))
	if err == nil || !strings.Contains(err.Error(), "parse existing instruction block") {
		t.Fatalf("ScanInstructionBlocks() error = %v, want parse existing instruction block error", err)
	}
}

func TestInstructionRenderBlockWritesInstructionSyntax(t *testing.T) {
	rendered := RenderInstructionBlock("daily", Instruction{
		Source:  InstructionSource{Path: "docs/daily.md", Mode: "vendor"},
		Targets: []string{"codex"},
	})

	requireInstructionContains(t, rendered, `[instructions."daily"]`)
	requireInstructionContains(t, rendered, `source = "docs/daily.md"`)
	requireInstructionContains(t, rendered, `targets = ["codex"]`)
}

func TestInstructionRenderBlockWritesTargetRenderingTables(t *testing.T) {
	rendered := RenderInstructionBlock("daily", Instruction{
		Source:  InstructionSource{Path: "docs/daily.md", Mode: "vendor"},
		Targets: []string{"antigravity-cli"},
		Scope:   "project",
		Target: map[string]InstructionRendering{
			"antigravity-cli": {RenderTo: "GEMINI.md"},
		},
	})

	requireInstructionContains(t, rendered, `[instructions."daily"]`)
	requireInstructionContains(t, rendered, `[instructions."daily".target."antigravity-cli"]`)
	requireInstructionContains(t, rendered, `render_to = "GEMINI.md"`)
}

func TestInstructionTargetTableRemovalAndTargetReplacement(t *testing.T) {
	block := `[instructions."project"]
source = "AGENTS.md"
targets = ["codex", "claude-code"]

[instructions."project".target."codex"]
render_to = "AGENTS.md"

[instructions."project".target."claude-code"]
render_to = "CLAUDE.md"
`

	updated := RemoveInstructionTargetTables(block, "project", []string{"codex"})
	updated = ReplaceInstructionTargets(updated, "project", []string{"claude-code"})

	requireInstructionContains(t, updated, `targets = ["claude-code"]`)
	requireInstructionContains(t, updated, `[instructions."project".target."claude-code"]`)
	requireInstructionNotContains(t, updated, `[instructions."project".target."codex"]`)
}

func TestInstructionTargetTableRemovalIgnoresHeaderLikeStringValues(t *testing.T) {
	block := `[instructions."project"]
source = "AGENTS.md"
targets = ["codex", "claude-code"]
note = "[instructions.\"project\".target.\"codex\"]"

[instructions."project".target."codex"]
render_to = "AGENTS.md"

[instructions."project".target."claude-code"]
render_to = "CLAUDE.md"
`

	updated := RemoveInstructionTargetTables(block, "project", []string{"codex"})

	requireInstructionContains(t, updated, `note = "[instructions.\"project\".target.\"codex\"]"`)
	requireInstructionContains(t, updated, `[instructions."project".target."claude-code"]`)
	requireInstructionNotContains(t, updated, `render_to = "AGENTS.md"`)
}

func requireInstructionContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Fatalf("content = %q, want fragment %q", content, fragment)
	}
}

func requireInstructionNotContains(t *testing.T, content string, fragment string) {
	t.Helper()
	if strings.Contains(content, fragment) {
		t.Fatalf("content = %q, did not want fragment %q", content, fragment)
	}
}
