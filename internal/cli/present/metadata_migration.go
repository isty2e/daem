package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/workflow/migrate"
)

func PrintMetadataMigration(writer io.Writer, mode string, plan migrate.MetadataDisclosure) {
	fmt.Fprintf(writer, "metadata conversion (%s): %s\n", mode, plan.Action)
	manifestLabel := "manifest"
	if plan.Action == "rollback" || plan.Action == "finalize" {
		manifestLabel = "recorded manifest"
	}
	fmt.Fprintf(writer, "  %s: %s\n  data directory: %s\n", manifestLabel, Escape(plan.ManifestPath), Escape(plan.DataDir))
	fmt.Fprintf(writer, "  participating State namespaces: %d\n", len(plan.Statefiles))
	for _, path := range plan.Statefiles {
		fmt.Fprintf(writer, "    %s\n", Escape(path))
	}
	fmt.Fprintf(writer, "  checked metadata namespaces (including unchanged or absent): %d\n", len(plan.Targets))
	for _, path := range plan.Targets {
		fmt.Fprintf(writer, "    %s\n", Escape(path))
	}
	if len(plan.CleanupPaths) != 0 {
		fmt.Fprintf(writer, "  inactive metadata directories to clean: %d\n", len(plan.CleanupPaths))
		for _, path := range plan.CleanupPaths {
			fmt.Fprintf(writer, "    %s\n", Escape(path))
		}
	}
	for _, reason := range plan.CleanupDeferred {
		fmt.Fprintf(writer, "  cleanup deferred: %s\n", Escape(reason))
	}
	if len(plan.ReferenceAssertions) != 0 {
		fmt.Fprintln(writer, "  operator-supplied historical lookup assertions:")
		for _, assertion := range plan.ReferenceAssertions {
			fmt.Fprintf(writer, "    identity: %s (%s)\n", Escape(assertion.IdentityKey), Escape(assertion.Witness))
			fmt.Fprintf(writer, "      supplied: %s\n      observed: %s\n", Escape(assertion.InputPath), Escape(assertion.ObservedPath))
		}
		fmt.Fprintln(writer, "  current filesystem checks do not prove these were the historical locations")
	}
	fmt.Fprintln(writer, "  installed outputs and host packages are unchanged")
}

type metadataReferenceAssertion struct {
	InputPath    string `json:"input_path"`
	IdentityKey  string `json:"identity_key"`
	Witness      string `json:"witness"`
	ObservedPath string `json:"observed_path"`
}

func PrintMetadataMigrationJSON(writer io.Writer, mode string, plan migrate.MetadataDisclosure, executeErr error) error {
	result := struct {
		SchemaVersion       int                          `json:"schema_version"`
		Mode                string                       `json:"mode"`
		Action              string                       `json:"action"`
		ManifestPath        string                       `json:"manifest_path"`
		DataDir             string                       `json:"data_dir"`
		Statefiles          []string                     `json:"statefiles"`
		Targets             []string                     `json:"targets"`
		ReferenceAssertions []metadataReferenceAssertion `json:"reference_assertions,omitempty"`
		CleanupPaths        []string                     `json:"cleanup_targets,omitempty"`
		CleanupDeferred     []string                     `json:"cleanup_deferred,omitempty"`
		Error               string                       `json:"error,omitempty"`
	}{
		SchemaVersion: contractversion.MetadataMigrationJSON,
		Mode:          mode, Action: plan.Action, ManifestPath: plan.ManifestPath,
		DataDir: plan.DataDir, Statefiles: plan.Statefiles, Targets: plan.Targets,
		CleanupPaths: plan.CleanupPaths, CleanupDeferred: plan.CleanupDeferred,
	}
	for _, assertion := range plan.ReferenceAssertions {
		result.ReferenceAssertions = append(result.ReferenceAssertions, metadataReferenceAssertion{
			InputPath: assertion.InputPath, IdentityKey: assertion.IdentityKey,
			Witness: assertion.Witness, ObservedPath: assertion.ObservedPath,
		})
	}
	if executeErr != nil {
		result.Error = executeErr.Error()
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
