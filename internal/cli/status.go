package cli

import (
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	statusworkflow "github.com/isty2e/daem/internal/workflow/status"
)

func prepareStatusInvocation(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"status"}, stderr)
	var targetValues targetFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	checkOutput := flags.Bool("check", false, "exit non-zero when status is not clean")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "target to inspect; may be repeated")
	plan := prepareGoArguments(flags, args)
	response := newCommandResponse("status", plan.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"status"}, stdout, 0)
			return 0
		}
		if err := plan.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"status"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, unexpectedArgumentDiagnostic(flags.Arg(0), targetValues))
		}
		if err := validatePresentationFlags("status", plan.output == requestedJSON, *verbose, false); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}

		result, err := statusworkflow.Run(options.context, statusworkflow.CommandInput{
			ManifestPath: *manifestPath,
			LockfilePath: "",
			TargetValues: targetValues.strings(),
		})
		if err != nil {
			diagnostic := humanFailureDiagnostic(err).WithPrefix("status failed: ")
			if guidance, present := missingManifestInitGuidance(*manifestPath, err); present {
				diagnostic = diagnostic.WithGuidance(guidance)
			}
			if errors.Is(err, targetselection.ErrInvalid) {
				diagnostic = diagnostic.WithGuidance(targetSelectionGuidance(result.ManifestPath, *manifestPath)...)
			}
			return response.failure(operationFailure, diagnostic)
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		mcpStatuses, err := clipresent.MCPStatusesFrom(result.MCPProjections)
		if err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("status failed: render MCP status: "))
		}

		if plan.output == requestedJSON {
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
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("status failed: write json: "))
			}
			return statusCheckExitCode(result, *checkOutput)
		}
		if result.LockfileMissing {
			fmt.Fprintf(stdout, "lockfile: missing %s\n", humanDiagnosticText(result.LockfilePath))
			printLockCommandHint(stdout, result.ManifestPath, *manifestPath)
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
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("status failed: render result: "))
		}
		if result.Reconciliation.HasLockReadinessErrors() {
			printLockCommandHint(stdout, result.ManifestPath, *manifestPath)
		}
		clipresent.PrintDiagnosticsWithOptions(stdout, result.Diagnostics, humanOptions)
		if clipresent.PrintReconciliationGuidance(stdout, result.Reconciliation) && !*verbose {
			printReconciliationInspectionHint(stdout, result.ManifestPath, *manifestPath, targetValues, *verbose)
		}
		return statusCheckExitCode(result, *checkOutput)
	}}
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
