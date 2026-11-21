package codec_test

import (
	"bytes"
	"errors"
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

func TestAgentConfigEditsPreserveNeighboringBytes(t *testing.T) {
	const prefix = `version = 1
targets = ["pi"]
[defaults]
scope = "project"
`
	const authored = `[['agent_config']]
"id" = "git-settings"
path = "settings.json"

[agent_config.source]
git = "https://github.com/example/settings.git"
path = "settings.json"
ref = "main" # selected line
`
	const adjacent = "\n# local settings follow\n"
	const sibling = `[[agent_config]]
id = "local-settings"
path = "other.json"
source = "./other.json"`
	const replacement = `[[agent_config]]
id = "git-settings"
targets = ["pi"]
scope = "project"
path = "settings.json"
source = "updated.json"
`
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	before := []byte(crlf(prefix + authored + adjacent + sibling))
	prior, err := manifest.Decode(before)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manifest.Decode([]byte(prefix + replacement))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		edit declarationcodec.AgentConfigEdit
		text string
	}{
		{declarationcodec.AgentConfigRemoval("git-settings"), ""},
		{declarationcodec.AgentConfigReplacement(updated.AgentConfigs()[0]), replacement},
	} {
		after, err := tc.edit.ApplyBlock(before)
		want := crlf(prefix) + tc.text + crlf(adjacent+sibling)
		if err != nil || string(after) != want {
			t.Fatalf("edit = %q, %v; want %q", after, err, want)
		}
		decoded, err := manifest.Decode(after)
		if err != nil || !tc.edit.MatchesDeclarations(prior.AgentConfigs(), decoded.AgentConfigs()) {
			t.Fatalf("unexpected declarations: %v", err)
		}

		changedSibling := strings.Replace(want, "./other.json", "./changed.json", 1)
		changed, err := manifest.Decode([]byte(changedSibling))
		if err != nil {
			t.Fatal(err)
		}
		if tc.edit.MatchesDeclarations(prior.AgentConfigs(), changed.AgentConfigs()) {
			t.Fatal("accepted an edit that also changed a sibling's source")
		}

		for _, image := range []struct {
			text     string
			accepted bool
		}{
			{want, true},
			{crlf(prefix) + tc.text + crlf(sibling), true},
			{changedSibling, false},
			{crlf(prefix) + tc.text + "\r\n" + crlf(sibling), false},
		} {
			if ok, err := tc.edit.MatchesCaptureAfter(before, []byte(image.text)); err != nil || ok != image.accepted {
				t.Fatalf("recovery accepted=%t, err=%v for %q", ok, err, image.text)
			}
		}
	}
}

func TestAgentConfigBlockSelectionDistinguishesSyntaxErrors(t *testing.T) {
	const row = "[[agent_config]]\nid = 'chosen'\n"
	for _, content := range []string{"agent_config = []", row + row} {
		if _, err := declarationcodec.AgentConfigRemoval("chosen").ApplyBlock([]byte(content)); !errors.Is(err, declarationcodec.ErrAgentConfigBlockSelection) {
			t.Fatal("missing or ambiguous selection not reported", err)
		}
	}
	if _, err := declarationcodec.AgentConfigRemoval("chosen").ApplyBlock([]byte(row + "path = 'unterminated")); err == nil || errors.Is(err, declarationcodec.ErrAgentConfigBlockSelection) {
		t.Fatal("malformed block treated as a missing selection", err)
	}
}

func TestUnrelatedAuthoringAndImportEditsPreserveAgentConfigRows(t *testing.T) {
	const originalBlock = `[[agent_config]]
id = "existing-settings"
path = "settings.json"
source = "/workbench/settings.json"
ownership = "document"
syntax = "jsonc"

`
	original := []byte(`version = 1
targets = ["pi"]
[defaults]
scope = "global"

` + originalBlock + `[instructions.existing]
source = "/workbench/existing.md"
targets = ["pi"]
scope = "global"
`)
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
	if !bytes.Contains(instructionEdit.Content, []byte(originalBlock)) {
		t.Fatal("instruction add changed the agent_config row")
	}
	if env, err := manifest.Decode(instructionEdit.Content); err != nil || len(env.AgentConfigs()) != 1 {
		t.Fatalf("instruction add did not retain exactly one agent_config: %v", err)
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
	if !bytes.Contains(merged, []byte(originalBlock)) {
		t.Fatal("import merge changed the agent_config row")
	}
	if env, err := manifest.Decode(merged); err != nil || len(env.AgentConfigs()) != 1 {
		t.Fatalf("import merge did not retain exactly one agent_config: %v", err)
	}
}
