package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/workflow/migrate"
)

func PrintStateMigration(writer io.Writer, mode string, plan migrate.StateDisclosure) {
	fmt.Fprintf(writer, "state migration (%s): %s\n", mode, plan.Action)
	fmt.Fprintf(writer, "  manifest: %s\n  from: %s\n  to: %s\n", Escape(plan.ManifestPath), Escape(plan.SourceStatefile), Escape(plan.DestinationStatefile))
	fmt.Fprintf(writer, "  output registry: %s\n  carrier registry: %s\n", Escape(plan.OutputRegistryPath), Escape(plan.CarrierRegistryPath))
	if plan.Action == "migrate" || plan.Action == "migrated" {
		fmt.Fprintf(writer, "  retained: %d managed paths, %d aggregates\n", plan.ManagedPaths, plan.ManagedAggregates)
		fmt.Fprintf(writer, "  authority transfer: %d output claims, %d global carrier claims\n", plan.OutputClaims, plan.CarrierClaims)
	}
	fmt.Fprintln(writer, "  installed outputs and host packages are unchanged; legacy caches are retained")
}

type stateMigrationCounts struct {
	OutputClaims      int `json:"output_claims"`
	CarrierClaims     int `json:"carrier_claims"`
	ManagedPaths      int `json:"managed_paths"`
	ManagedAggregates int `json:"managed_aggregates"`
}

func PrintStateMigrationJSON(writer io.Writer, mode string, plan migrate.StateDisclosure, executeErr error) error {
	result := struct {
		*stateMigrationCounts
		SchemaVersion        int    `json:"schema_version"`
		Mode                 string `json:"mode"`
		Action               string `json:"action"`
		ManifestPath         string `json:"manifest_path"`
		SourceStatefile      string `json:"source_statefile"`
		DestinationStatefile string `json:"destination_statefile"`
		OutputRegistryPath   string `json:"output_registry_path"`
		CarrierRegistryPath  string `json:"carrier_registry_path"`
		Error                string `json:"error,omitempty"`
	}{
		SchemaVersion: contractversion.StateMigrationJSON, Mode: mode, Action: plan.Action,
		ManifestPath: plan.ManifestPath, SourceStatefile: plan.SourceStatefile,
		DestinationStatefile: plan.DestinationStatefile,
		OutputRegistryPath:   plan.OutputRegistryPath, CarrierRegistryPath: plan.CarrierRegistryPath,
	}
	if plan.Action == "migrate" || plan.Action == "migrated" || plan.Action == "already_migrated" {
		result.stateMigrationCounts = &stateMigrationCounts{
			OutputClaims: plan.OutputClaims, CarrierClaims: plan.CarrierClaims,
			ManagedPaths: plan.ManagedPaths, ManagedAggregates: plan.ManagedAggregates,
		}
	}
	if executeErr != nil {
		result.Error = executeErr.Error()
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
