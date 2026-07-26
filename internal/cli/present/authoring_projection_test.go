package clipresent

import (
	"slices"
	"strings"
	"testing"

	authoringworkflow "github.com/isty2e/daem/internal/workflow/authoring"
)

func TestAuthoringProjectionKeepsGrammarAxesAndPublicNotesExact(t *testing.T) {
	addCases := []struct {
		kind     AuthoringResourceKind
		noteText string
	}{
		{kind: AuthoringResourceInstructions, noteText: "host files are written only by apply"},
		{kind: AuthoringResourceSkill, noteText: "host files are written only by apply"},
		{kind: AuthoringResourceSkillGroup, noteText: "host files are written only by apply"},
		{kind: AuthoringResourceHook, noteText: "managed hook aggregates"},
		{kind: AuthoringResourceMCPServer, noteText: "locked projection"},
		{kind: AuthoringResourceExtension, noteText: "separately admitted host route"},
	}
	for _, test := range addCases {
		t.Run("add_"+string(test.kind), func(t *testing.T) {
			warnings := []string{"warning-canary"}
			result := authoringworkflow.OperationResult{
				ManifestPath:  "/repo/daem.toml",
				ResourceID:    "example",
				ChangeKind:    "insert",
				ManifestBlock: "[[resource]]\n",
				Warnings:      warnings,
				Mode:          authoringworkflow.AuthoringModeDryRun,
			}

			summary := AuthoringChangeFrom("add", AuthoringOperationAdd, test.kind, result)
			payload := ManifestAuthoringJSONFrom(AuthoringOperationAdd, test.kind, result)
			if summary.ResourceID != string(test.kind)+"/example" ||
				summary.PlannedBlock != result.ManifestBlock ||
				!slices.Equal(summary.Warnings, warnings) ||
				!strings.Contains(summary.NextStepNote, test.noteText) ||
				!summary.DryRun {
				t.Fatalf("add summary = %#v, want exact %s projection", summary, test.kind)
			}
			if payload.Command != "add" ||
				payload.Operation != "add" ||
				payload.Mode != string(authoringworkflow.AuthoringModeDryRun) ||
				len(payload.Changes) != 1 ||
				payload.Changes[0].ResourceID != string(test.kind)+"/example" ||
				payload.Changes[0].ManifestBlock != result.ManifestBlock ||
				!slices.Equal(payload.Warnings, warnings) {
				t.Fatalf("add JSON payload = %#v, want exact %s projection", payload, test.kind)
			}

			warnings[0] = "mutated"
			result.Warnings[0] = "mutated-again"
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
				ManifestBlock: "must-not-appear",
				Warnings:      []string{"must-not-appear"},
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
