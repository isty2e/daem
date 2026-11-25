package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/contractversion"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

func captureRecoveryWarnings(plan *workflowadopt.AgentConfigCaptureRecovery) []string {
	warnings := []string{
		"This invocation only recovers the named source/declaration transaction; it does not read new host values or retry capture.",
		"Approval covers these named files. Other manifests or programs may read them; exclusive source ownership is not established.",
		"Recovery can restore or finalize literal plaintext data, including credentials. No file contents are printed.",
	}
	switch plan.Disposition() {
	case "finalize":
		warnings = append(warnings, "Finalization keeps the recorded changes. Review the usual lock and apply steps if you intend to deploy them.")
	case "rollback":
		warnings = append(warnings, "Rollback restores the previous state. Preview a new capture with --dry-run only if you still want one.")
	}
	if plan.RetiresDeclaration() {
		warnings = append(warnings, "Retirement recovery changes only the declaration; the old source remains untouched.")
	}
	return warnings
}

func AgentConfigCaptureRecoveryJSON(plan *workflowadopt.AgentConfigCaptureRecovery, mode string) ([]byte, error) {
	if err := checkAgentConfigIdentifier("manifest", plan.ManifestPath()); err != nil {
		return nil, err
	}
	if err := checkCaptureSource(plan.SourcePath(), plan.SourceReferent()); err != nil {
		return nil, err
	}
	refs, err := captureJSONReferences(plan.KnownReferences())
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(struct {
		SchemaVersion int                    `json:"schema_version"`
		Command       string                 `json:"command"`
		Operation     string                 `json:"operation"`
		Mode          string                 `json:"mode"`
		Recovery      string                 `json:"recovery"`
		Fingerprint   string                 `json:"fingerprint"`
		Manifest      string                 `json:"manifest"`
		Source        string                 `json:"source"`
		Referent      string                 `json:"referent"`
		Warnings      []string               `json:"warnings"`
		References    []captureReferenceJSON `json:"known_references"`
	}{
		SchemaVersion: contractversion.AgentConfigCaptureJSON, Command: "import " + AgentConfigResourceName, Operation: "recovery", Mode: mode,
		Recovery: plan.Disposition(), Fingerprint: plan.Fingerprint(), Manifest: plan.ManifestPath(), Source: plan.SourcePath(), Referent: plan.SourceReferent(),
		Warnings: captureRecoveryWarnings(plan), References: refs,
	}, "", "  ")
}

func PrintAgentConfigCaptureRecovery(out io.Writer, plan *workflowadopt.AgentConfigCaptureRecovery) error {
	if _, err := fmt.Fprintf(out, "Capture recovery: %s\n  manifest: %s\n  source: %s\n  referent: %s\n", plan.Disposition(), humanErrorDetail(plan.ManifestPath()), humanErrorDetail(plan.SourcePath()), humanErrorDetail(plan.SourceReferent())); err != nil {
		return err
	}
	for _, warning := range captureRecoveryWarnings(plan) {
		if _, err := fmt.Fprintln(out, "  warning: "+warning); err != nil {
			return err
		}
	}
	if err := printCaptureReaders(out, plan.KnownReferences()); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "fingerprint: "+plan.Fingerprint())
	return err
}
