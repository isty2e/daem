package codec

import (
	"strings"
	"testing"
)

func TestExtensionScanAndRenderExtensionBlock(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"
source = { marketplace = "context7@market" }

[[mcp_server]]
name = "later"
`)
	blocks, err := ScanExtensionBlocks(original)
	if err != nil {
		t.Fatalf("ScanExtensionBlocks returned error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("len(blocks) = %d, want 1", len(blocks))
	}
	extension := blocks[0].Extension
	if extension.ID != "context7-managed" ||
		extension.Carrier != "claude-code-plugin" ||
		extension.Source.Marketplace != "context7@market" {
		t.Fatalf("extension = %#v, want Claude Code marketplace extension", extension)
	}
	if strings.Contains(string(original[blocks[0].Start:blocks[0].End]), "[[mcp_server]]") {
		t.Fatalf("scanned range included following mcp_server block")
	}

	rendered := RenderExtensionBlock(Extension{
		ID:      "context7-managed",
		Carrier: "claude-code-plugin",
		Targets: []string{"claude-code"},
		Scope:   "project",
		Source:  ExtensionSource{Marketplace: "context7@market"},
	})
	for _, want := range []string{
		"[[extension]]",
		`id = "context7-managed"`,
		`carrier = "claude-code-plugin"`,
		`targets = ["claude-code"]`,
		`scope = "project"`,
		`source = { marketplace = "context7@market" }`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered = %q, want %q", rendered, want)
		}
	}
}

func TestExtensionScanAndRenderExtensionHostSource(t *testing.T) {
	original := []byte(`[[extension]]
id = "formatter-managed"
carrier = "opencode-plugin"
targets = ["opencode"]
source = { host_source = "@acme/opencode-formatter" }
`)
	blocks, err := ScanExtensionBlocks(original)
	if err != nil {
		t.Fatalf("ScanExtensionBlocks returned error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("len(blocks) = %d, want 1", len(blocks))
	}
	if blocks[0].Extension.Source.HostSource != "@acme/opencode-formatter" ||
		blocks[0].Extension.Source.Marketplace != "" {
		t.Fatalf("source = %#v, want host_source only", blocks[0].Extension.Source)
	}

	rendered := RenderExtensionBlock(Extension{
		ID:      "formatter-managed",
		Carrier: "opencode-plugin",
		Targets: []string{"opencode"},
		Source:  ExtensionSource{HostSource: "@acme/opencode-formatter"},
	})
	for _, want := range []string{
		"[[extension]]",
		`carrier = "opencode-plugin"`,
		`source = { host_source = "@acme/opencode-formatter" }`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered = %q, want %q", rendered, want)
		}
	}
	if strings.Contains(rendered, "marketplace") {
		t.Fatalf("rendered = %q, want no marketplace field", rendered)
	}
}

func TestExtensionScanBlocksKeepsNestedSourceTableInExtensionBlock(t *testing.T) {
	original := []byte(`[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"

[extension.source]
marketplace = "context7@market"

[[extension]]
id = "other"
carrier = "claude-code-plugin"
source = { marketplace = "other@market" }
`)

	blocks, err := ScanExtensionBlocks(original)
	if err != nil {
		t.Fatalf("ScanExtensionBlocks returned error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(blocks))
	}
	if blocks[0].Extension.Source.Marketplace != "context7@market" {
		t.Fatalf("first source = %#v, want context7@market", blocks[0].Extension.Source)
	}
	block := string(original[blocks[0].Start:blocks[0].End])
	if !strings.Contains(block, "[extension.source]") {
		t.Fatalf("block = %q, want nested source table included", block)
	}
	if strings.Contains(block, `id = "other"`) {
		t.Fatalf("block = %q, want following extension excluded", block)
	}
}

func TestExtensionRelationIdentityAndSourceReferenceAreCodecOwned(t *testing.T) {
	left := Extension{
		ID: "left", Carrier: "claude_plugin", Targets: []string{"claude-code"}, Scope: "project",
		Source: ExtensionSource{Marketplace: "official"},
	}
	right := left
	right.ID = "right"
	if !SameExtensionRelation(left, right) {
		t.Fatal("declaration ID incorrectly participated in relation identity")
	}
	if got := left.Source.Ref(); got != "official" {
		t.Fatalf("Source.Ref = %q", got)
	}
}
