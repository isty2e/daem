package clipresent

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	authoringworkflow "github.com/isty2e/daem/internal/workflow/authoring"
)

func TestAuthoringWarningsSharePublicLabelsWithoutRewritingEvidence(t *testing.T) {
	const plain = "source instructions/mcp_server must remain literal"
	change, err := authoringworkflow.BuildAddMCPServerChange(authoringworkflow.ManifestDocument{
		Content: []byte("version=1\ntargets=[\"claude-code\"]\n"),
	}, authoringworkflow.AddMCPServerRequest{Name: "mcp_server", Command: "npx", Args: []string{"-y", "@scope/server"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Warnings) != 1 || change.Warnings[0].Resource.Kind() != entity.KindMCPServer {
		t.Fatalf("producer lost warning resource: %#v", change.Warnings)
	}
	result := authoringworkflow.OperationResult{Warnings: append(change.Warnings, authoringworkflow.Warning{Detail: plain})}
	human := AuthoringChangeFrom("add", AuthoringOperationAdd, AuthoringResourceMCPServer, result)
	wire := ManifestAuthoringJSONFrom(AuthoringOperationAdd, AuthoringResourceMCPServer, result)
	if len(human.Warnings) != 2 || !slices.Equal(human.Warnings, wire.Warnings) ||
		!strings.HasPrefix(wire.Warnings[0], `mcp-server "mcp_server" uses floating delegated`) || wire.Warnings[1] != plain {
		t.Fatalf("warning projection changed evidence: human=%q JSON=%q", human.Warnings, wire.Warnings)
	}
}

func TestAuthoringProjectionKeepsGrammarAxesAndPublicNotesExact(t *testing.T) {
	addCases := []struct {
		kind     AuthoringResourceKind
		public   string
		noteText string
	}{
		{kind: AuthoringResourceInstructions, public: "instruction", noteText: "host files are written only by apply"},
		{kind: AuthoringResourceSkill, public: "skill", noteText: "host files are written only by apply"},
		{kind: AuthoringResourceSkillGroup, public: "skill-group", noteText: "host files are written only by apply"},
		{kind: AuthoringResourceHook, public: "hook", noteText: "managed hook aggregates"},
		{kind: AuthoringResourceMCPServer, public: "mcp-server", noteText: "locked projection"},
		{kind: AuthoringResourceExtension, public: "extension", noteText: "separately admitted host route"},
	}
	for _, test := range addCases {
		t.Run("add_"+string(test.kind), func(t *testing.T) {
			warnings := []authoringworkflow.Warning{{Detail: "warning-canary"}}
			result := authoringworkflow.OperationResult{
				ManifestPath:  "/repo/daem.toml",
				ResourceID:    "example",
				ChangeKind:    "insert",
				Content:       []byte("[[resource]]\n"),
				ManifestBlock: "[[resource]]\n",
				Warnings:      warnings,
				Mode:          authoringworkflow.AuthoringModeDryRun,
			}

			summary := AuthoringChangeFrom("add", AuthoringOperationAdd, test.kind, result)
			payload := ManifestAuthoringJSONFrom(AuthoringOperationAdd, test.kind, result)
			if summary.ResourceID != test.public+"/example" ||
				summary.PlannedBlock != result.ManifestBlock ||
				!slices.Equal(summary.Warnings, []string{"warning-canary"}) ||
				!strings.Contains(summary.NextStepNote, test.noteText) ||
				!summary.DryRun {
				t.Fatalf("add summary = %#v, want exact %s projection", summary, test.kind)
			}
			if payload.Command != "add" ||
				payload.Operation != "add" ||
				payload.Mode != string(authoringworkflow.AuthoringModeDryRun) ||
				len(payload.Changes) != 1 ||
				payload.Changes[0].ResourceID != test.public+"/example" ||
				payload.Changes[0].Resource.Kind != test.public ||
				payload.Changes[0].ManifestBlock != result.ManifestBlock ||
				!slices.Equal(payload.Warnings, []string{"warning-canary"}) {
				t.Fatalf("add JSON payload = %#v, want exact %s projection", payload, test.kind)
			}

			warnings[0].Detail = "mutated"
			result.Warnings[0].Detail = "mutated-again"
			if summary.Warnings[0] != "warning-canary" || payload.Warnings[0] != "warning-canary" {
				t.Fatalf("authoring projections retained workflow warning storage: summary=%#v payload=%#v", summary.Warnings, payload.Warnings)
			}
		})
	}
}

func TestAuthoringRemoveProjectionOmitsAddOnlyContent(t *testing.T) {
	removeCases := []struct {
		kind     AuthoringResourceKind
		noteText string
	}{
		{kind: AuthoringResourceInstructions, noteText: "apply reconciles managed state"},
		{kind: AuthoringResourceSkill, noteText: "apply reconciles managed state"},
		{kind: AuthoringResourceHook, noteText: "managed hook aggregates"},
		{kind: AuthoringResourceMCPServer, noteText: "removes the managed projection"},
		{kind: AuthoringResourceExtension, noteText: "carrier uninstall"},
	}
	for _, test := range removeCases {
		t.Run("remove_"+string(test.kind), func(t *testing.T) {
			result := authoringworkflow.OperationResult{
				ManifestPath:  "/repo/daem.toml",
				ResourceID:    "example",
				ChangeKind:    "remove",
				Original:      []byte("[[resource]]\n"),
				ManifestBlock: "must-not-appear",
				Warnings:      []authoringworkflow.Warning{{Detail: "must-not-appear"}},
				Mode:          authoringworkflow.AuthoringModeWrite,
			}

			summary := AuthoringChangeFrom("removed", AuthoringOperationRemove, test.kind, result)
			payload := ManifestAuthoringJSONFrom(AuthoringOperationRemove, test.kind, result)
			if summary.PlannedBlock != "" ||
				len(summary.Warnings) != 0 ||
				!strings.Contains(summary.NextStepNote, test.noteText) ||
				summary.DryRun {
				t.Fatalf("remove summary = %#v, want no add-only content", summary)
			}
			if payload.Command != "remove" ||
				payload.Operation != "remove" ||
				len(payload.Changes) != 1 ||
				payload.Changes[0].ManifestBlock != "" ||
				len(payload.Warnings) != 0 {
				t.Fatalf("remove JSON payload = %#v, want no add-only content", payload)
			}
		})
	}
}

func TestUnknownAuthoringOperationDoesNotInventLifecycleNote(t *testing.T) {
	summary := AuthoringChangeFrom(
		"future",
		AuthoringOperation("future"),
		AuthoringResourceExtension,
		authoringworkflow.OperationResult{ResourceID: "example"},
	)
	if summary.NextStepNote != "" || summary.PlannedBlock != "" || len(summary.Warnings) != 0 {
		t.Fatalf("unknown authoring operation invented semantics: %#v", summary)
	}
}
