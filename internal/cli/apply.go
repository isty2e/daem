package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func runApply(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"apply"}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"apply"}, stderr)

	var targetValues targetFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "compute and print actions without writing")
	showDiff := flags.Bool("diff", false, "show file content diffs with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	yes := flags.Bool("yes", false, "bypass confirmation when applying mutations")
	manageExisting := flags.Bool("manage-existing", false, "record exact-match unmanaged outputs as managed without writing those outputs")
	flags.Var(&targetValues, "target", "target to reconcile; may be repeated")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 0 {
		printUnexpectedArgumentWithTargetHint(stderr, flags.Arg(0), targetValues)
		return 2
	}

	if *dryRun && *yes {
		fmt.Fprintln(stderr, "apply failed: --dry-run and --yes are mutually exclusive")
		return 2
	}
	if *showDiff && !*dryRun {
		fmt.Fprintln(stderr, "apply failed: --diff requires --dry-run")
		return 2
	}
	if err := validatePresentationFlags("apply", *jsonOutput, *verbose, *showDiff); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}
	if *jsonOutput && !*dryRun && !*yes {
		fmt.Fprintln(stderr, "apply failed: --json requires --dry-run or --yes")
		return 2
	}

	interactiveConfirmation := !*dryRun && !*yes
	if interactiveConfirmation && !options.confirmation.allowsInteractiveAuthorization() {
		printInteractiveConfirmationRequired(stderr, "apply", "apply")
		fmt.Fprintln(stderr, "next: run daem apply --dry-run first, then rerun with --yes when the plan is acceptable")
		return 2
	}

	if *dryRun {
		planning, err := applyworkflow.PlanDryRun(options.context, applyworkflow.CommandInput{
			ManifestPath:           *manifestPath,
			LockfilePath:           "",
			TargetValues:           targetValues.strings(),
			RelationObservations:   options.applyExecuteOptions.RelationObservations,
			ManageUnmanagedMatches: *manageExisting,
		})
		if err != nil {
			fmt.Fprintf(stderr, "apply failed: %s\n", humanDiagnosticError(err))
			printApplyWorkflowHints(stderr, *manifestPath, planning.CommandResult, err)
			printUnsupportedCapabilityHint(stderr, err)
			return 1
		}
		mcpStatuses, err := clipresent.MCPStatusesFrom(planning.MCPProjections)
		if err != nil {
			fmt.Fprintf(stderr, "apply failed: inspect MCP projection status: %s\n", humanDiagnosticError(err))
			return 1
		}

		if *jsonOutput {
			jsonInput := clipresent.PlanJSONInput{
				Command:        "apply",
				Mode:           "dry-run",
				LockfilePath:   planning.LockfilePath,
				LockOnly:       clipresent.LockOnlyResourcesFrom(planning.LockOnly),
				Reconciliation: planning.Reconciliation,
				Diagnostics:    planning.Diagnostics,
				MCPStatuses:    mcpStatuses,
			}
			if err := clipresent.PrintPlanJSON(stdout, jsonInput); err != nil {
				fmt.Fprintf(stderr, "apply failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			if planning.Reconciliation.HasErrors() {
				return 1
			}
			return 0
		}

		humanOptions := clipresent.HumanOptions{Verbose: *verbose}
		clipresent.PrintDryRunPlanWithOptions(stdout, planning.Reconciliation, humanOptions)
		clipresent.PrintLockOnlyResourceSummary(stdout, clipresent.LockOnlyResourcesFrom(planning.LockOnly))
		clipresent.PrintMCPStatusesWithOptions(stdout, mcpStatuses, humanOptions)
		clipresent.PrintRelationActionsWithOptions(stdout, planning.Reconciliation.Relations(), humanOptions)
		clipresent.PrintCarrierAdoptionActionsWithOptions(stdout, planning.Reconciliation.CarrierAdoptions(), humanOptions)
		clipresent.PrintCarrierAbsenceActionsWithOptions(stdout, planning.Reconciliation.CarrierAbsences(), humanOptions)
		clipresent.PrintDelegateActionsWithOptions(stdout, planning.Reconciliation.Delegates(), humanOptions)
		if planning.Reconciliation.HasLockReadinessErrors() {
			printLockCommandHint(stdout, planning.ManifestPath)
		}
		clipresent.PrintDiagnosticsWithOptions(stdout, planning.Diagnostics, humanOptions)
		if planning.Reconciliation.HasErrors() {
			return 1
		}
		if *showDiff {
			diffs, err := applyworkflow.BuildDiffs(options.context, planning)
			if err != nil {
				fmt.Fprintf(stderr, "apply failed: %s\n", humanDiagnosticError(err))
				return 1
			}
			clipresent.PrintDryRunDiffs(stdout, clipresent.DryRunDiffsFrom(diffs))
		}

		return 0
	}

	readinessPlanning, err := applyworkflow.PlanWrite(options.context, applyworkflow.CommandInput{
		ManifestPath:           *manifestPath,
		LockfilePath:           "",
		TargetValues:           targetValues.strings(),
		RelationObservations:   options.applyExecuteOptions.RelationObservations,
		ManageUnmanagedMatches: *manageExisting,
	})
	if err != nil {
		if *jsonOutput && readinessPlanning.ReconciliationReady {
			mcpStatuses, statusErr := clipresent.MCPStatusesFrom(readinessPlanning.MCPProjections)
			if statusErr != nil {
				fmt.Fprintf(stderr, "apply failed: inspect MCP projection status: %s\n", humanDiagnosticError(statusErr))
				return 1
			}
			jsonInput := clipresent.ApplyResultJSONInput{
				ActionCount:    0,
				StatefilePath:  readinessPlanning.StatefilePath,
				LockOnly:       clipresent.LockOnlyResourcesFrom(readinessPlanning.LockOnly),
				Reconciliation: readinessPlanning.Reconciliation,
				MCPStatuses:    mcpStatuses,
				Diagnostics:    readinessPlanning.Diagnostics,
				Err:            err,
			}
			if jsonErr := clipresent.PrintApplyResultJSON(stdout, jsonInput); jsonErr != nil {
				fmt.Fprintf(stderr, "apply failed: write json: %s\n", humanDiagnosticError(jsonErr))
				return 1
			}
			return 1
		}
		fmt.Fprintf(stderr, "apply failed: %s\n", humanDiagnosticError(err))
		clipresent.PrintRelationActionsWithOptions(stderr, readinessPlanning.Reconciliation.Relations(), clipresent.HumanOptions{Verbose: *verbose})
		clipresent.PrintCarrierAdoptionActionsWithOptions(stderr, readinessPlanning.Reconciliation.CarrierAdoptions(), clipresent.HumanOptions{Verbose: *verbose})
		clipresent.PrintCarrierAbsenceActionsWithOptions(stderr, readinessPlanning.Reconciliation.CarrierAbsences(), clipresent.HumanOptions{Verbose: *verbose})
		printApplyWorkflowHints(stderr, *manifestPath, readinessPlanning.CommandResult, err)
		if readinessPlanning.Reconciliation.HasLockReadinessErrors() {
			printLockCommandHint(stderr, readinessPlanning.ManifestPath)
		}
		printUnsupportedCapabilityHint(stderr, err)
		return 1
	}
	defer readinessPlanning.Close()
	mcpStatuses, err := clipresent.MCPStatusesFrom(readinessPlanning.MCPProjections)
	if err != nil {
		fmt.Fprintf(stderr, "apply failed: inspect MCP projection status: %s\n", humanDiagnosticError(err))
		return 1
	}
	relationActionsDisclosed := false
	carrierAbsencesDisclosed := false
	humanOptions := clipresent.HumanOptions{Verbose: *verbose}
	if interactiveConfirmation {
		clipresent.PrintLockOnlyResourceSummary(stdout, clipresent.LockOnlyResourcesFrom(readinessPlanning.LockOnly))
		clipresent.PrintRelationActionsWithOptions(stdout, readinessPlanning.Reconciliation.Relations(), humanOptions)
		relationActionsDisclosed = true
		clipresent.PrintCarrierAdoptionActionsWithOptions(stdout, readinessPlanning.Reconciliation.CarrierAdoptions(), humanOptions)
		clipresent.PrintCarrierAbsenceActionsWithOptions(stdout, readinessPlanning.Reconciliation.CarrierAbsences(), humanOptions)
		carrierAbsencesDisclosed = true
		clipresent.PrintActionPlanWithOptions(stdout, "apply", readinessPlanning.Reconciliation, humanOptions)
		clipresent.PrintDelegateActionsWithOptions(stdout, readinessPlanning.Reconciliation.Delegates(), humanOptions)
		clipresent.PrintDiagnosticsWithOptions(stdout, readinessPlanning.Diagnostics, humanOptions)
		if applyConfirmationRequired(readinessPlanning.CommandResult) {
			confirmed, err := options.confirmation.prompt("apply")
			if err != nil {
				printConfirmationFailure(stderr, "apply", err)
				return 1
			}
			if !confirmed {
				fmt.Fprintln(stderr, "apply canceled")
				return 1
			}
		}
	}
	if !interactiveConfirmation && !*jsonOutput {
		clipresent.PrintDiagnosticsWithOptions(stdout, readinessPlanning.Diagnostics, humanOptions)
		clipresent.PrintRelationActionsWithOptions(stdout, readinessPlanning.Reconciliation.Relations(), humanOptions)
		relationActionsDisclosed = true
		clipresent.PrintCarrierAdoptionActionsWithOptions(stdout, readinessPlanning.Reconciliation.CarrierAdoptions(), humanOptions)
		clipresent.PrintCarrierAbsenceActionsWithOptions(stdout, readinessPlanning.Reconciliation.CarrierAbsences(), humanOptions)
		carrierAbsencesDisclosed = true
		clipresent.PrintDelegateActionsWithOptions(stdout, readinessPlanning.Reconciliation.Delegates(), humanOptions)
	}

	progress := newApplyProgressRenderer(*jsonOutput, stderr, options)
	executeOptions := options.applyExecuteOptions
	executeOptions.ExecuteEvents = progress.Sink()
	executeOptions.PlanWasDisclosed = interactiveConfirmation
	result, err := applyworkflow.ExecuteWithOptions(options.context, readinessPlanning, executeOptions)
	progress.Close()
	delegateAttemptInputs := clipresent.DelegateAttemptInputsFrom(result.DelegateAttempts)
	if err != nil {
		if *jsonOutput {
			jsonInput := clipresent.ApplyResultJSONInput{
				ActionCount:            result.ActionCount,
				StatefilePath:          result.StatefilePath,
				LockOnly:               clipresent.LockOnlyResourcesFrom(result.LockOnly),
				Reconciliation:         result.Reconciliation,
				ExecutionAttempted:     true,
				CarrierAdoptionResults: result.CarrierAdoptionResults,
				HostRouteAttempts:      result.HostRouteAttempts,
				MCPStatuses:            mcpStatuses,
				Diagnostics:            result.Diagnostics,
				Err:                    err,
			}
			jsonInput.DelegateAttempts = delegateAttemptInputs
			if jsonErr := clipresent.PrintApplyResultJSON(stdout, jsonInput); jsonErr != nil {
				fmt.Fprintf(stderr, "apply failed: write json: %s\n", humanDiagnosticError(jsonErr))
				return 1
			}
			return 1
		}
		clipresent.PrintDelegateAttemptsWithOptions(stderr, delegateAttemptInputs, humanOptions)
		clipresent.PrintCarrierAdoptionResultsWithOptions(
			stderr,
			result.Reconciliation.CarrierAdoptions(),
			result.CarrierAdoptionResults,
			false,
			humanOptions,
		)
		if presentErr := clipresent.PrintHostRouteAttemptsWithOptions(stderr, result.HostRouteAttempts, humanOptions); presentErr != nil {
			fmt.Fprintf(stderr, "apply diagnostics failed: %s\n", humanDiagnosticError(presentErr))
		}
		fmt.Fprintf(stderr, "apply failed: %s\n", humanDiagnosticError(err))
		printUnsupportedCapabilityHint(stderr, err)
		return 1
	}

	if *jsonOutput {
		jsonInput := clipresent.ApplyResultJSONInput{
			ActionCount:            result.ActionCount,
			StatefilePath:          result.StatefilePath,
			LockOnly:               clipresent.LockOnlyResourcesFrom(result.LockOnly),
			Reconciliation:         result.Reconciliation,
			ExecutionAttempted:     true,
			CarrierAdoptionResults: result.CarrierAdoptionResults,
			HostRouteAttempts:      result.HostRouteAttempts,
			Diagnostics:            result.Diagnostics,
		}
		jsonInput.DelegateAttempts = delegateAttemptInputs
		if err := clipresent.PrintApplyResultJSON(stdout, jsonInput); err != nil {
			fmt.Fprintf(stderr, "apply failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "applied: %d actions\n", result.ActionCount)
	clipresent.PrintCarrierAdoptionResultsWithOptions(
		stdout,
		result.Reconciliation.CarrierAdoptions(),
		result.CarrierAdoptionResults,
		true,
		humanOptions,
	)
	clipresent.PrintPlanResultWithOptions(stdout, result.Reconciliation, humanOptions)
	if !relationActionsDisclosed {
		clipresent.PrintRelationActionsWithOptions(stdout, result.Reconciliation.Relations(), humanOptions)
	}
	if !carrierAbsencesDisclosed {
		clipresent.PrintCarrierAbsenceActionsWithOptions(stdout, result.Reconciliation.CarrierAbsences(), humanOptions)
	}
	if err := clipresent.PrintHostRouteAttemptsWithOptions(stdout, result.HostRouteAttempts, humanOptions); err != nil {
		fmt.Fprintf(stderr, "apply failed: render result: %s\n", humanDiagnosticError(err))
		return 1
	}
	clipresent.PrintDelegateAttemptsWithOptions(stdout, delegateAttemptInputs, humanOptions)
	clipresent.PrintLockOnlyResourceSummary(stdout, clipresent.LockOnlyResourcesFrom(result.LockOnly))
	if *verbose {
		fmt.Fprintf(stdout, "statefile: %s\n", humanDiagnosticText(result.StatefilePath))
	}

	return 0
}

func applyConfirmationRequired(planning applyworkflow.CommandResult) bool {
	if len(planning.Reconciliation.MutatingDecisions()) != 0 {
		return true
	}
	for _, action := range planning.Reconciliation.Relations() {
		if action.InvokesHostRoute() {
			return true
		}
	}
	for _, action := range planning.Reconciliation.CarrierAdoptions() {
		if action.StateOnly() {
			return true
		}
	}
	for _, action := range planning.Reconciliation.Delegates() {
		if action.SchedulesAttempt() {
			return true
		}
	}
	return false
}

func printApplyWorkflowHints(output io.Writer, manifestPath string, result applyworkflow.CommandResult, err error) {
	printMissingManifestInitHint(output, manifestPath, err)
	if errors.Is(err, targetselection.ErrInvalid) {
		printTargetSelectionHint(output, result.ManifestPath)
	}
	if errors.Is(err, applyworkflow.ErrReadLockfile) && errors.Is(err, os.ErrNotExist) {
		printLockCommandHint(output, result.ManifestPath)
	}
}
