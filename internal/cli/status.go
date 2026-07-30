package cli

import (
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	statusworkflow "github.com/isty2e/daem/internal/workflow/status"
)

func runStatus(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"status"}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"status"}, stderr)

	var targetValues targetFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	checkOutput := flags.Bool("check", false, "exit non-zero when status is not clean")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "target to inspect; may be repeated")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 0 {
		printUnexpectedArgumentWithTargetHint(stderr, flags.Arg(0), targetValues)
		return 2
	}
	if err := validatePresentationFlags("status", *jsonOutput, *verbose, false); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}

	result, err := statusworkflow.Run(options.context, statusworkflow.CommandInput{
		ManifestPath: *manifestPath,
		LockfilePath: "",
		TargetValues: targetValues.strings(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "status failed: %s\n", humanDiagnosticError(err))
		printMissingManifestInitHint(stderr, *manifestPath, err)
		if errors.Is(err, targetselection.ErrInvalid) {
			printTargetSelectionHint(stderr, result.ManifestPath)
		}
		printUnsupportedCapabilityHint(stderr, err)
		return 1
	}
	mcpStatuses, err := clipresent.MCPStatusesFrom(result.MCPProjections)
	if err != nil {
		fmt.Fprintf(stderr, "status failed: render MCP status: %s\n", humanDiagnosticError(err))
		return 1
	}

	if *jsonOutput {
		mode := "status"
		if *checkOutput {
			mode = "check"
		}
		if err := clipresent.PrintPlanJSON(stdout, clipresent.PlanJSONInput{
			Command:           "status",
			Mode:              mode,
			LockfilePath:      result.LockfilePath,
			LockfileMissing:   result.LockfileMissing,
			LockOnly:          clipresent.LockOnlyResourcesFrom(result.LockOnly),
			Reconciliation:    result.Reconciliation,
			HostRouteAttempts: result.HostRouteAttempts,
			Diagnostics:       result.Diagnostics,
			MCPStatuses:       mcpStatuses,
		}); err != nil {
			fmt.Fprintf(stderr, "status failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return statusCheckExitCode(result, *checkOutput)
	}
	if result.LockfileMissing {
		fmt.Fprintf(stdout, "lockfile: missing %s\n", humanDiagnosticText(result.LockfilePath))
		printLockCommandHint(stdout, result.ManifestPath)
	}
	humanOptions := clipresent.HumanOptions{Verbose: *verbose}
	clipresent.PrintStatusPlanWithOptions(stdout, result.Reconciliation, humanOptions)
	clipresent.PrintStatusLockOnlyResourcesWithOptions(stdout, clipresent.LockOnlyResourcesFrom(result.LockOnly), humanOptions)
	clipresent.PrintMCPStatusesWithOptions(stdout, mcpStatuses, humanOptions)
	clipresent.PrintRelationActionsWithOptions(stdout, result.Reconciliation.Relations(), humanOptions)
	clipresent.PrintRelationOrderActionsWithOptions(stdout, result.Reconciliation.RelationOrders(), humanOptions)
	clipresent.PrintCarrierAdoptionActionsWithOptions(stdout, result.Reconciliation.CarrierAdoptions(), humanOptions)
	clipresent.PrintCarrierAbsenceActionsWithOptions(stdout, result.Reconciliation.CarrierAbsences(), humanOptions)
	if err := clipresent.PrintHostRouteAttemptsWithOptions(stdout, result.HostRouteAttempts, humanOptions); err != nil {
		fmt.Fprintf(stderr, "status failed: render result: %s\n", humanDiagnosticError(err))
		return 1
	}
	if result.Reconciliation.HasLockReadinessErrors() {
		printLockCommandHint(stdout, result.ManifestPath)
	}
	clipresent.PrintDiagnosticsWithOptions(stdout, result.Diagnostics, humanOptions)

	return statusCheckExitCode(result, *checkOutput)
}

func statusCheckExitCode(result statusworkflow.CommandResult, check bool) int {
	if !check {
		return 0
	}
	if result.LockfileMissing || len(result.Reconciliation.PendingManagedPaths()) != 0 || len(result.Reconciliation.PendingAggregates()) != 0 ||
		result.HasBlockedRelationActions() ||
		result.Reconciliation.HasNonExactRelationOrders() ||
		result.Reconciliation.HasBlockedCarrierAdoptions() ||
		result.Reconciliation.HasBlockedCarrierAbsences() {
		return 1
	}
	return 0
}
