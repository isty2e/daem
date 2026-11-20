package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/contractversion"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
)

type outputReleaseDisclosure struct {
	Manifest              string   `json:"manifest,omitempty"`
	RecordedManifests     []string `json:"recorded_manifests"`
	WithheldManifestCount int      `json:"withheld_manifest_count,omitempty"`
	Locations             []string `json:"locations"`
	WithheldLocationCount int      `json:"withheld_location_count,omitempty"`
	ClaimCount            int      `json:"claim_count"`
	BindingCount          int      `json:"binding_count"`
}

func visibleRecoveryReference(value string) bool {
	return value != "" && !shellArgumentRequiresReconstruction(value)
}

func releaseDisclosure(plan recoverworkflow.OutputReleasePlan) outputReleaseDisclosure {
	result := outputReleaseDisclosure{
		RecordedManifests: []string{}, Locations: []string{}, ClaimCount: plan.ClaimCount, BindingCount: plan.BindingCount,
		WithheldLocationCount: plan.WithheldLocationCount,
	}
	if visibleRecoveryReference(plan.ManifestPath) {
		result.Manifest = plan.ManifestPath
	}
	for _, manifest := range plan.RecordedManifests {
		if visibleRecoveryReference(manifest) {
			result.RecordedManifests = append(result.RecordedManifests, manifest)
		} else {
			result.WithheldManifestCount++
		}
	}
	for _, location := range plan.Locations {
		if visibleRecoveryReference(location) {
			result.Locations = append(result.Locations, location)
		} else {
			result.WithheldLocationCount++
		}
	}
	return result
}

func PrintOutputReleaseJSON(output io.Writer, mode string, plan recoverworkflow.OutputReleasePlan, phase recoverworkflow.OutputReleasePhase, err error) error {
	payload := struct {
		SchemaVersion int                                `json:"schema_version"`
		Command       string                             `json:"command"`
		Mode          string                             `json:"mode"`
		Phase         recoverworkflow.OutputReleasePhase `json:"phase"`
		AuthorityKind string                             `json:"authority_kind"`
		HasErrors     bool                               `json:"has_errors"`
		Release       outputReleaseDisclosure            `json:"output_ownership_release"`
		Errors        []string                           `json:"errors,omitempty"`
	}{
		SchemaVersion: contractversion.RecoveryJSON, Command: "recover", Mode: mode, Phase: phase,
		AuthorityKind: "output_ownership_release", HasErrors: err != nil, Release: releaseDisclosure(plan),
	}
	if err != nil {
		payload.Errors = []string{outputReleaseFailure(phase, err).HumanText()}
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func PrintOutputRelease(output io.Writer, plan recoverworkflow.OutputReleasePlan, phase recoverworkflow.OutputReleasePhase, err error) {
	if err != nil {
		fmt.Fprintln(output, outputReleaseFailure(phase, err).HumanText())
		return
	}
	switch phase {
	case recoverworkflow.OutputReleaseNothing:
		fmt.Fprintln(output, "no recorded output ownership to release for the selected state file")
	case recoverworkflow.OutputReleaseReleased:
		fmt.Fprintf(output, "released output ownership (%d claims, %d document bindings); host files were not changed\n", plan.ClaimCount, plan.BindingCount)
	case recoverworkflow.OutputReleasePlanned:
		disclosure := releaseDisclosure(plan)
		fmt.Fprintf(output, "would release all recorded output ownership for the selected state file (%d claims, %d document bindings)\n", plan.ClaimCount, plan.BindingCount)
		for _, manifest := range disclosure.RecordedManifests {
			fmt.Fprintf(output, "manifest in the recorded ownership: %s\n", Quote(manifest))
		}
		for _, location := range disclosure.Locations {
			fmt.Fprintf(output, "output: %s\n", Quote(location))
		}
		if disclosure.WithheldManifestCount+disclosure.WithheldLocationCount > 0 {
			fmt.Fprintf(output, "references not shown: %d manifests, %d locations\n", disclosure.WithheldManifestCount, disclosure.WithheldLocationCount)
		}
		fmt.Fprintln(output, "This leaves host files in place but ends the ownership protection for these outputs.")
	}
}

func outputReleaseFailure(phase recoverworkflow.OutputReleasePhase, err error) FailureDiagnostic {
	switch phase {
	case recoverworkflow.OutputReleaseUnconfirmed:
		return NewFailureDiagnostic("recover failed: daem could not confirm output ownership release").WithGuidance(
			InstructionGuidance("preserve the records and preview the release again with the same manifest selection"),
		)
	case recoverworkflow.OutputReleaseReleased:
		return NewFailureDiagnostic("output ownership was released and host files were not changed; final cleanup did not complete")
	default:
		return RecoverPlanningDiagnostic(err, HumanOptions{})
	}
}
