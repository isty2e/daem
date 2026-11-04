package codec_test

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/declaration/manifest"
)

func TestScanHostConfigBlocksKeepsNestedSourceAndFindsNextRow(t *testing.T) {
	content := []byte(`version = 1
targets = ["pi"]

[[host_config]]
id = "git-settings"
path = "config.toml"

[host_config.source]
git = "https://github.com/example/settings.git"
path = "config.toml"
ref = "main"

[[host_config]]
id = "local-settings"
path = "settings.json"
source = "/workbench/settings.json"
`)
	blocks, err := declarationcodec.ScanHostConfigBlocks(content)
	if err != nil {
		t.Fatalf("ScanHostConfigBlocks() error = %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(blocks))
	}
	if blocks[0].HostConfig.ID != "git-settings" ||
		blocks[0].HostConfig.Source.Source.Git != "https://github.com/example/settings.git" ||
		blocks[0].HostConfig.Source.Source.Path != "config.toml" ||
		blocks[0].HostConfig.Source.Source.Ref != "main" {
		t.Fatalf("first block = %#v, want decoded nested Git source", blocks[0].HostConfig)
	}
	firstBlock := string(content[blocks[0].Start:blocks[0].End])
	if !bytes.Contains([]byte(firstBlock), []byte("[host_config.source]")) ||
		bytes.Contains([]byte(firstBlock), []byte(`id = "local-settings"`)) {
		t.Fatalf("first block range = %q, want nested source and no following row", firstBlock)
	}
	if key := blocks[1].HostConfig.Key(); key != (declaration.Key{Kind: declaration.KindHostConfig, Name: "local-settings"}) {
		t.Fatalf("second row key = %#v, want host_config/local-settings", key)
	}
}

func TestUnrelatedAuthoringAndImportEditsPreserveHostConfigRows(t *testing.T) {
	original := []byte(`version = 1
targets = ["pi"]
[defaults]
scope = "global"

[[host_config]]
id = "existing-settings"
path = "settings.json"
source = "/workbench/settings.json"
ownership = "document"
syntax = "jsonc"

[instructions.existing]
source = "/workbench/existing.md"
targets = ["pi"]
scope = "global"
`)
	originalBlocks, err := declarationcodec.ScanHostConfigBlocks(original)
	if err != nil || len(originalBlocks) != 1 {
		t.Fatalf("ScanHostConfigBlocks(original) = %#v, %v", originalBlocks, err)
	}
	originalBlock := bytes.Clone(original[originalBlocks[0].Start:originalBlocks[0].End])
	header := declaration.ManifestHeader{
		Targets: []string{"pi"}, Defaults: declaration.Defaults{Scope: "global"},
	}
	instructionEdit, err := declarationcodec.ApplyInstructionAdd(original, header, "new", declarationcodec.Instruction{
		Source:  declarationcodec.InstructionSource{Path: "/workbench/new.md", Mode: "vendor"},
		Targets: []string{"pi"}, Scope: "global",
	})
	if err != nil {
		t.Fatalf("ApplyInstructionAdd() error = %v", err)
	}
	assertHostConfigRowUnchanged(t, originalBlock, instructionEdit.Content)
	if _, err := manifest.Decode(instructionEdit.Content); err != nil {
		t.Fatalf("manifest with unrelated authoring edit no longer decodes: %v", err)
	}

	body, err := declarationcodec.RenderImportManifestBody(declarationcodec.ImportManifestBody{
		Skills: []declarationcodec.ImportManifestSkill{{
			Name: "imported", Source: declarationcodec.ImportManifestSource{Path: "skills/imported", Mode: "vendor"},
			Targets: []string{"pi"}, Scope: "project", InstallMode: "copy",
		}},
	})
	if err != nil {
		t.Fatalf("RenderImportManifestBody() error = %v", err)
	}
	merged := declarationcodec.AppendImportManifestBody(original, body)
	assertHostConfigRowUnchanged(t, originalBlock, merged)
	if _, err := manifest.Decode(merged); err != nil {
		t.Fatalf("manifest with unrelated import merge no longer decodes: %v", err)
	}
}

func assertHostConfigRowUnchanged(t *testing.T, expected []byte, content []byte) {
	t.Helper()
	blocks, err := declarationcodec.ScanHostConfigBlocks(content)
	if err != nil || len(blocks) != 1 {
		t.Fatalf("ScanHostConfigBlocks() = %#v, %v", blocks, err)
	}
	if !bytes.Equal(content[blocks[0].Start:blocks[0].End], expected) {
		t.Fatalf("host_config row changed:\n%s\nwant:\n%s", content[blocks[0].Start:blocks[0].End], expected)
	}
}
