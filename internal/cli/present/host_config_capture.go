package clipresent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

type CaptureKeyJSON struct {
	Path          string `json:"path"`
	BeforePresent bool   `json:"before_present"`
	AfterPresent  bool   `json:"after_present"`
	Before        string `json:"before,omitempty"`
	After         string `json:"after,omitempty"`
}

type CaptureReferenceJSON struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
}

type HostConfigCaptureJSON struct {
	SchemaVersion int                    `json:"schema_version"`
	Command       string                 `json:"command"`
	Mode          string                 `json:"mode"`
	Declaration   string                 `json:"declaration"`
	Fingerprint   string                 `json:"fingerprint"`
	Source        string                 `json:"source"`
	Referent      string                 `json:"referent"`
	Initial       bool                   `json:"initial"`
	Ownership     string                 `json:"ownership"`
	Retire        bool                   `json:"retire_declaration"`
	SourceBytes   int                    `json:"source_bytes"`
	Keys          []CaptureKeyJSON       `json:"keys"`
	References    []CaptureReferenceJSON `json:"known_references"`
	Warnings      []string               `json:"warnings"`
	NextSteps     []string               `json:"next_steps"`
}

func HostConfigCaptureDisclosure(ctx context.Context, plan *workflowadopt.HostConfigCapturePlan, mode string) (HostConfigCaptureJSON, error) {
	result := HostConfigCaptureJSON{
		SchemaVersion: 1, Command: "import host-config", Mode: mode,
		Declaration: humanErrorDetail(plan.Config().ID().Name()), Fingerprint: plan.Fingerprint(),
		Source: humanErrorDetail(plan.SourcePath()), Referent: humanErrorDetail(plan.SourceReferent()),
		Initial: plan.Initial(), Ownership: string(plan.Config().Ownership()), Retire: plan.RetiresDeclaration(), SourceBytes: len(plan.SourceAfter()),
		Keys: []CaptureKeyJSON{}, References: []CaptureReferenceJSON{},
		Warnings: []string{
			"Approval covers this named source file, not exclusive ownership or only this declaration.",
			"Other manifests or programs may read this file; not all readers are enumerated.",
			"Literal values, including credentials, may be stored as plaintext or committed to version control. Display masking does not certify secret-free data.",
			"Only source and declaration artifacts change; host files, lock, state and ownership records do not.",
		},
		NextSteps: []string{},
	}
	for _, command := range [][]string{{"lock"}, {"apply", "--dry-run"}, {"apply"}} {
		args := command[1:]
		if plan.Initial() && command[0] == "apply" {
			args = append([]string{"--manage-existing"}, args...)
		}
		text, err := ManifestCommand(command[:1], plan.ManifestPath(), HumanOptions{}, args...)
		if err != nil {
			return HostConfigCaptureJSON{}, err
		}
		result.NextSteps = append(result.NextSteps, humanErrorDetail(text))
	}
	if plan.RetiresDeclaration() {
		result.Warnings = append(result.Warnings, "The missing whole document retires this declaration; the old source file is left untouched.")
	} else if !plan.Initial() {
		result.Warnings = append(result.Warnings, "Capture replaces selected desired source values; it does not save their previous intent for later restoration.")
	}
	if plan.NarrowingHandoff() {
		result.Warnings = append(result.Warnings, "After capture, narrow ownership with lock, preview and apply. Change desired source values only in a separate later update.")
	}
	values, err := plan.SelectedValues(ctx)
	if err != nil {
		return HostConfigCaptureJSON{}, err
	}
	for _, row := range values {
		result.Keys = append(result.Keys, CaptureKeyJSON{
			Path: humanErrorDetail(row.Key.String()), BeforePresent: row.BeforePresent, AfterPresent: row.AfterPresent,
			Before: nativeReplacementValueDisplay(row.Key, row.Before), After: nativeReplacementValueDisplay(row.Key, row.After),
		})
	}
	for _, reference := range plan.KnownReferences() {
		result.References = append(result.References, CaptureReferenceJSON{Resource: humanErrorDetail(reference.Resource), Path: humanErrorDetail(reference.Path)})
	}
	return result, nil
}

func (disclosure HostConfigCaptureJSON) JSON() ([]byte, error) {
	return json.MarshalIndent(disclosure, "", "  ")
}

func PrintHostConfigCapture(out io.Writer, disclosure HostConfigCaptureJSON, diff bool) error {
	if _, err := fmt.Fprintf(out, "Capture %s (%s)\n  source: %s\n  referent: %s\n", disclosure.Declaration, disclosure.Mode, disclosure.Source, disclosure.Referent); err != nil {
		return err
	}
	for _, reference := range disclosure.References {
		if _, err := fmt.Fprintf(out, "  known reader: %s (%s)\n", reference.Resource, reference.Path); err != nil {
			return err
		}
	}
	for _, warning := range disclosure.Warnings {
		if _, err := fmt.Fprintln(out, "  warning: "+warning); err != nil {
			return err
		}
	}
	for _, row := range disclosure.Keys {
		before, after := "<absent>", "<absent; release this source key>"
		if row.BeforePresent {
			before = row.Before
		}
		if row.AfterPresent {
			after = row.After
		}
		if diff {
			if _, err := fmt.Fprintf(out, "@@ %s @@\n- %s\n+ %s\n", row.Path, before, after); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(out, "  %s\n    previous source: %s\n    captured: %s\n", row.Path, before, after); err != nil {
			return err
		}
	}
	if disclosure.Ownership == "document" && !disclosure.Retire {
		if _, err := fmt.Fprintf(out, "  source: %d bytes (raw whole-document contents omitted)\n", disclosure.SourceBytes); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out, "fingerprint: "+disclosure.Fingerprint); err != nil {
		return err
	}
	for _, step := range disclosure.NextSteps {
		if _, err := fmt.Fprintln(out, "  next: "+step); err != nil {
			return err
		}
	}
	return nil
}
