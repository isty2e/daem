package codec_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func TestAgentConfigBlockPreservesPreviouslyAdmittedCaptureEscapes(t *testing.T) {
	const name = "prefs\b\t\n\f\r\x01\x1b\x7fwith escapes.toml"
	provenance, err := source.NewLocalSource(name, source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	config, err := agentconfig.New(agentconfig.Spec{
		Name: "capture", Source: provenance, Target: target.TargetCodex, Scope: target.ScopeProject, Path: "config.toml",
	})
	if err != nil {
		t.Fatal(err)
	}
	block, err := declarationcodec.RenderAgentConfigBlock(config)
	if err != nil {
		t.Fatal(err)
	}
	// Capture recovery checks the exact previously rendered declaration bytes.
	if !strings.HasSuffix(block, "source = "+strconv.Quote(name)+"\n") {
		t.Fatalf("capture source no longer uses its previous spelling: %q", block)
	}
	environment, err := manifest.Decode([]byte("version = 1\ntargets = ['codex']\n" + block))
	if err != nil || len(environment.AgentConfigs()) != 1 || environment.AgentConfigs()[0] != config {
		t.Fatalf("previous spelling no longer decodes to the exact declaration: %v", err)
	}
}

func TestScanAgentConfigBlocksKeepsNestedSourceAndFindsNextRow(t *testing.T) {
	content := []byte(`version = 1
targets = ["pi"]

[[agent_config]]
id = "git-settings"
path = "config.toml"

[agent_config.source]
git = "https://github.com/example/settings.git"
path = "config.toml"
ref = "main"

[[agent_config]]
id = "local-settings"
path = "settings.json"
source = "/workbench/settings.json"
`)
	blocks, err := declarationcodec.ScanAgentConfigBlocks(content)
	if err != nil {
		t.Fatalf("ScanHostConfigBlocks() error = %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(blocks))
	}
	if blocks[0].AgentConfig.ID != "git-settings" ||
		blocks[0].AgentConfig.Source.Source.Git != "https://github.com/example/settings.git" ||
		blocks[0].AgentConfig.Source.Source.Path != "config.toml" ||
		blocks[0].AgentConfig.Source.Source.Ref != "main" {
		t.Fatalf("first block = %#v, want decoded nested Git source", blocks[0].AgentConfig)
	}
	firstBlock := string(content[blocks[0].Start:blocks[0].End])
	if !bytes.Contains([]byte(firstBlock), []byte("[agent_config.source]")) ||
		bytes.Contains([]byte(firstBlock), []byte(`id = "local-settings"`)) {
		t.Fatalf("first block range = %q, want nested source and no following row", firstBlock)
	}
	if key := blocks[1].AgentConfig.Key(); key != (declaration.Key{Kind: declaration.KindAgentConfig, Name: "local-settings"}) {
		t.Fatalf("second row key = %#v, want agent-config/local-settings", key)
	}
}

func TestUnrelatedAuthoringAndImportEditsPreserveAgentConfigRows(t *testing.T) {
	original := []byte(`version = 1
targets = ["pi"]
[defaults]
scope = "global"

[[agent_config]]
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
	originalBlocks, err := declarationcodec.ScanAgentConfigBlocks(original)
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
	assertAgentConfigRowUnchanged(t, originalBlock, instructionEdit.Content)
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
	assertAgentConfigRowUnchanged(t, originalBlock, merged)
	if _, err := manifest.Decode(merged); err != nil {
		t.Fatalf("manifest with unrelated import merge no longer decodes: %v", err)
	}
}

func assertAgentConfigRowUnchanged(t *testing.T, expected []byte, content []byte) {
	t.Helper()
	blocks, err := declarationcodec.ScanAgentConfigBlocks(content)
	if err != nil || len(blocks) != 1 {
		t.Fatalf("ScanHostConfigBlocks() = %#v, %v", blocks, err)
	}
	if !bytes.Equal(content[blocks[0].Start:blocks[0].End], expected) {
		t.Fatalf("agent_config row changed:\n%s\nwant:\n%s", content[blocks[0].Start:blocks[0].End], expected)
	}
}
