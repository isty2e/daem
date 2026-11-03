package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

type HostConfigCaptureRecoveryJSON struct {
	SchemaVersion int                    `json:"schema_version"`
	Command       string                 `json:"command"`
	Mode          string                 `json:"mode"`
	Recovery      string                 `json:"recovery"`
	Fingerprint   string                 `json:"fingerprint"`
	Manifest      string                 `json:"manifest"`
	Source        string                 `json:"source"`
	Referent      string                 `json:"referent"`
	Warnings      []string               `json:"warnings"`
	References    []CaptureReferenceJSON `json:"known_references"`
}

func HostConfigCaptureRecoveryDisclosure(plan *workflowadopt.HostConfigCaptureRecovery, mode string) HostConfigCaptureRecoveryJSON {
	result := HostConfigCaptureRecoveryJSON{1, "import host-config", mode, plan.Disposition(), plan.Fingerprint(), humanErrorDetail(plan.ManifestPath()), humanErrorDetail(plan.SourcePath()), humanErrorDetail(plan.SourceReferent()), []string{
		"This invocation only recovers the named source/declaration transaction; it does not read new host values or retry capture.",
		"Approval covers these named files. Other manifests or programs may read them; exclusive source ownership is not established.",
		"Recovery can restore or finalize literal plaintext data, including credentials. No file contents are printed.",
		"After recovery, run a separate dry-run to review a new capture.",
	}, []CaptureReferenceJSON{}}
	for _, reference := range plan.KnownReferences() {
		result.References = append(result.References, CaptureReferenceJSON{Resource: humanErrorDetail(reference.Resource), Path: humanErrorDetail(reference.Path)})
	}
	if plan.RetiresDeclaration() {
		result.Warnings = append(result.Warnings, "Retirement recovery changes only the declaration; the old source remains untouched.")
	}
	return result
}

func (disclosure HostConfigCaptureRecoveryJSON) JSON() ([]byte, error) {
	return json.MarshalIndent(disclosure, "", "  ")
}

func PrintHostConfigCaptureRecovery(out io.Writer, disclosure HostConfigCaptureRecoveryJSON) error {
	if _, err := fmt.Fprintf(out, "Capture recovery: %s\n  manifest: %s\n  source: %s\n  referent: %s\n", disclosure.Recovery, disclosure.Manifest, disclosure.Source, disclosure.Referent); err != nil {
		return err
	}
	for _, warning := range disclosure.Warnings {
		if _, err := fmt.Fprintln(out, "  warning: "+warning); err != nil {
			return err
		}
	}
	for _, reference := range disclosure.References {
		if _, err := fmt.Fprintf(out, "  known reader: %s (%s)\n", reference.Resource, reference.Path); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out, "fingerprint: "+disclosure.Fingerprint)
	return err
}
