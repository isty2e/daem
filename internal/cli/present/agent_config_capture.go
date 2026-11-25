package clipresent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/contractversion"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

type captureDisclosure struct {
	plan                *workflowadopt.AgentConfigCapturePlan
	values              []workflowadopt.AgentConfigCaptureValue
	warnings, nextSteps []string
}

func AgentConfigCaptureDisclosure(ctx context.Context, plan *workflowadopt.AgentConfigCapturePlan) (captureDisclosure, error) {
	values, err := plan.SelectedValues(ctx)
	if err != nil {
		return captureDisclosure{}, err
	}
	steps, err := captureNextSteps(plan.ManifestPath(), plan.Initial())
	if err != nil {
		return captureDisclosure{}, err
	}
	warnings := []string{
		"Approval covers this named source file, not exclusive ownership or only this declaration.",
		"Other manifests or programs may read this file; not all readers are enumerated.",
		"Literal values, including credentials, may be stored as plaintext or committed to version control. Display masking does not certify secret-free data.",
		"Only source and declaration artifacts change; host files, lock, state and ownership records do not.",
	}
	if plan.RetiresDeclaration() {
		warnings = append(warnings, "The missing whole document retires this declaration; the old source file is left untouched.")
	} else if !plan.Initial() {
		warnings = append(warnings, "Capture replaces selected desired source values; it does not save their previous intent for later restoration.")
	}
	if plan.NarrowingHandoff() {
		warnings = append(warnings, "After capture, narrow ownership with lock, preview and apply. Change desired source values only in a separate later update.")
	}
	return captureDisclosure{plan: plan, values: values, warnings: warnings, nextSteps: steps}, nil
}

func captureNextSteps(manifest string, initial bool) ([]string, error) {
	disclosable := checkAgentConfigIdentifier("manifest", manifest) == nil
	var steps []string
	for _, command := range [][]string{{"lock"}, {"apply", "--dry-run"}, {"apply"}} {
		args := command[1:]
		if initial && command[0] == "apply" {
			args = append([]string{"--manage-existing"}, args...)
		}
		if !disclosable {
			step := strings.Join(append([]string{command[0]}, args...), " ")
			steps = append(steps, "Run "+step+" with the same explicit manifest.")
			continue
		}
		step, err := ManifestCommand(command[:1], manifest, HumanOptions{}, args...)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

type captureKeyJSON struct {
	KeyPath       []string `json:"key_path"`
	BeforePresent bool     `json:"before_present"`
	AfterPresent  bool     `json:"after_present"`
	Before        string   `json:"before,omitempty"`
	After         string   `json:"after,omitempty"`
}

type captureReferenceJSON struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
}

func captureJSONReferences(readers []workflowadopt.AgentConfigSourceReference) ([]captureReferenceJSON, error) {
	refs := make([]captureReferenceJSON, 0, len(readers))
	for i, reader := range readers {
		if err := checkAgentConfigIdentifier(fmt.Sprintf("known_references[%d].resource", i), reader.Resource); err != nil {
			return nil, err
		}
		if err := checkAgentConfigIdentifier(fmt.Sprintf("known_references[%d].path", i), reader.Path); err != nil {
			return nil, err
		}
		refs = append(refs, captureReferenceJSON{reader.Resource, reader.Path})
	}
	return refs, nil
}

func (disclosure captureDisclosure) JSON(mode string) ([]byte, error) {
	plan := disclosure.plan
	name := plan.Config().ID().Name()
	if err := checkAgentConfigIdentifier("declaration", name); err != nil {
		return nil, err
	}
	if err := checkCaptureSource(plan.SourcePath(), plan.SourceReferent()); err != nil {
		return nil, err
	}
	refs, err := captureJSONReferences(plan.KnownReferences())
	if err != nil {
		return nil, err
	}

	keys := make([]captureKeyJSON, 0, len(disclosure.values))
	for i, row := range disclosure.values {
		segments, err := agentConfigKeySegments(fmt.Sprintf("keys[%d].key_path", i), row.Key)
		if err != nil {
			return nil, err
		}
		keys = append(keys, captureKeyJSON{
			KeyPath: segments, BeforePresent: row.BeforePresent, AfterPresent: row.AfterPresent,
			Before: agentConfigValueSnippet(row.Key, row.Before), After: agentConfigValueSnippet(row.Key, row.After),
		})
	}
	return json.MarshalIndent(struct {
		SchemaVersion int                    `json:"schema_version"`
		Command       string                 `json:"command"`
		Operation     string                 `json:"operation"`
		Mode          string                 `json:"mode"`
		Declaration   string                 `json:"declaration"`
		Fingerprint   string                 `json:"fingerprint"`
		Source        string                 `json:"source"`
		Referent      string                 `json:"referent"`
		Initial       bool                   `json:"initial"`
		Ownership     string                 `json:"ownership"`
		Retire        bool                   `json:"retire_declaration"`
		SourceBytes   int                    `json:"source_bytes"`
		Keys          []captureKeyJSON       `json:"keys"`
		References    []captureReferenceJSON `json:"known_references"`
		Warnings      []string               `json:"warnings"`
		NextSteps     []string               `json:"next_steps"`
	}{
		SchemaVersion: contractversion.AgentConfigCaptureJSON, Command: "import " + AgentConfigResourceName, Operation: "capture", Mode: mode,
		Declaration: name, Fingerprint: plan.Fingerprint(), Source: plan.SourcePath(), Referent: plan.SourceReferent(),
		Initial: plan.Initial(), Ownership: string(plan.Config().Ownership()), Retire: plan.RetiresDeclaration(), SourceBytes: len(plan.SourceAfter()),
		Keys: keys, References: refs, Warnings: disclosure.warnings, NextSteps: disclosure.nextSteps,
	}, "", "  ")
}

func checkCaptureSource(source, referent string) error {
	if err := checkAgentConfigIdentifier("source", source); err != nil {
		return err
	}
	return checkAgentConfigIdentifier("referent", referent)
}

func printCaptureReaders(out io.Writer, readers []workflowadopt.AgentConfigSourceReference) error {
	for _, reader := range readers {
		if _, err := fmt.Fprintf(out, "  known reader: %s (%s)\n", humanErrorDetail(reader.Resource), humanErrorDetail(reader.Path)); err != nil {
			return err
		}
	}
	return nil
}

func (disclosure captureDisclosure) Print(out io.Writer, diff bool) error {
	plan := disclosure.plan
	if _, err := fmt.Fprintf(out, "Capture %s (preview)\n  source: %s\n  referent: %s\n", humanErrorDetail(plan.Config().ID().Name()), humanErrorDetail(plan.SourcePath()), humanErrorDetail(plan.SourceReferent())); err != nil {
		return err
	}
	if err := printCaptureReaders(out, plan.KnownReferences()); err != nil {
		return err
	}
	for _, warning := range disclosure.warnings {
		if _, err := fmt.Fprintln(out, "  warning: "+warning); err != nil {
			return err
		}
	}
	for _, row := range disclosure.values {
		before, after := "<absent>", "<absent; release this source key>"
		if row.BeforePresent {
			before = humanAgentConfigValue(row.Key, row.Before)
		}
		if row.AfterPresent {
			after = humanAgentConfigValue(row.Key, row.After)
		}
		key := humanErrorDetail(row.Key.String())
		if diff {
			if _, err := fmt.Fprintf(out, "@@ %s @@\n- %s\n+ %s\n", key, before, after); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(out, "  %s\n    previous source: %s\n    captured: %s\n", key, before, after); err != nil {
			return err
		}
	}
	if plan.Config().Ownership() == "document" && !plan.RetiresDeclaration() {
		if _, err := fmt.Fprintf(out, "  source: %d bytes (raw whole-document contents omitted)\n", len(plan.SourceAfter())); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out, "fingerprint: "+plan.Fingerprint()); err != nil {
		return err
	}
	for _, step := range disclosure.nextSteps {
		if _, err := fmt.Fprintln(out, "  next: "+step); err != nil {
			return err
		}
	}
	return nil
}
