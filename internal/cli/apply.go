package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

const maximumVerboseApplyFailureEvidenceRunes = 4_096

type applyFailureStage string

const (
	applyFailurePlanning     applyFailureStage = "planning"
	applyFailureProjection   applyFailureStage = "projection"
	applyFailureDiff         applyFailureStage = "diff"
	applyFailureConfirmation applyFailureStage = "confirmation"
	applyFailureExecution    applyFailureStage = "execution"
	applyFailureDiagnostics  applyFailureStage = "diagnostics"
	applyFailureOutput       applyFailureStage = "output"
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
			printApplyFailure(stderr, err, applyFailurePlanning, planning.CommandResult, *verbose)
			printApplyWorkflowHints(stderr, *manifestPath, planning.CommandResult, err, *verbose)
			return 1
		}
		mcpStatuses, err := clipresent.MCPStatusesFrom(planning.MCPProjections)
		if err != nil {
			printApplyFailure(stderr, err, applyFailureProjection, applyworkflow.CommandResult{}, *verbose)
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
				markOutputFailureReported(stdout)
				printApplyFailure(stderr, err, applyFailureOutput, applyworkflow.CommandResult{}, false)
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
		clipresent.PrintRelationOrderActionsWithOptions(stdout, planning.Reconciliation.RelationOrders(), humanOptions)
		clipresent.PrintCarrierAdoptionActionsWithOptions(stdout, planning.Reconciliation.CarrierAdoptions(), humanOptions)
		clipresent.PrintCarrierAbsenceActionsWithOptions(stdout, planning.Reconciliation.CarrierAbsences(), humanOptions)
		clipresent.PrintDelegateActionsWithOptions(stdout, planning.Reconciliation.Delegates(), humanOptions)
		if planning.Reconciliation.HasLockReadinessErrors() {
			printApplyLockCommandHint(stdout, planning.ManifestPath, *verbose)
		}
		clipresent.PrintDiagnosticsWithOptions(stdout, planning.Diagnostics, humanOptions)
		if planning.Reconciliation.HasErrors() {
			return 1
		}
		if *showDiff {
			diffs, err := applyworkflow.BuildDiffs(options.context, planning)
			if err != nil {
				printApplyFailure(stderr, err, applyFailureDiff, applyworkflow.CommandResult{}, *verbose)
				return 1
			}
			if err := clipresent.PrintDryRunDiffs(
				options.context,
				stdout,
				clipresent.DryRunDiffReportFrom(diffs),
			); err != nil {
				printApplyFailure(stderr, err, applyFailureDiff, applyworkflow.CommandResult{}, *verbose)
				return 1
			}
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
				printApplyFailure(stderr, statusErr, applyFailureProjection, applyworkflow.CommandResult{}, false)
				return 1
			}
			failure := applyworkflow.ClassifyFailure(err, readinessPlanning.CommandResult)
			jsonInput := clipresent.ApplyResultJSONInput{
				ActionCount:    0,
				StatefilePath:  readinessPlanning.StatefilePath,
				LockOnly:       clipresent.LockOnlyResourcesFrom(readinessPlanning.LockOnly),
				Reconciliation: readinessPlanning.Reconciliation,
				MCPStatuses:    mcpStatuses,
				Diagnostics:    readinessPlanning.Diagnostics,
				Failure:        &failure,
			}
			if jsonErr := clipresent.PrintApplyResultJSON(stdout, jsonInput); jsonErr != nil {
				markOutputFailureReported(stdout)
				printApplyFailure(stderr, jsonErr, applyFailureOutput, applyworkflow.CommandResult{}, false)
				return 1
			}
			return 1
		}
		printApplyFailure(stderr, err, applyFailurePlanning, readinessPlanning.CommandResult, *verbose)
		clipresent.PrintActionPlanWithOptions(
			stderr,
			"apply",
			readinessPlanning.Reconciliation,
			clipresent.HumanOptions{Verbose: *verbose},
		)
		clipresent.PrintRelationActionsWithOptions(stderr, readinessPlanning.Reconciliation.Relations(), clipresent.HumanOptions{Verbose: *verbose})
		clipresent.PrintRelationOrderActionsWithOptions(stderr, readinessPlanning.Reconciliation.RelationOrders(), clipresent.HumanOptions{Verbose: *verbose})
		clipresent.PrintCarrierAdoptionActionsWithOptions(stderr, readinessPlanning.Reconciliation.CarrierAdoptions(), clipresent.HumanOptions{Verbose: *verbose})
		clipresent.PrintCarrierAbsenceActionsWithOptions(stderr, readinessPlanning.Reconciliation.CarrierAbsences(), clipresent.HumanOptions{Verbose: *verbose})
		printApplyWorkflowHints(stderr, *manifestPath, readinessPlanning.CommandResult, err, *verbose)
		if readinessPlanning.Reconciliation.HasLockReadinessErrors() {
			printApplyLockCommandHint(stderr, readinessPlanning.ManifestPath, *verbose)
		}
		return 1
	}
	defer readinessPlanning.Close()
	mcpStatuses, err := clipresent.MCPStatusesFrom(readinessPlanning.MCPProjections)
	if err != nil {
		printApplyFailure(stderr, err, applyFailureProjection, applyworkflow.CommandResult{}, *verbose)
		return 1
	}
	relationActionsDisclosed := false
	relationOrdersDisclosed := false
	carrierAbsencesDisclosed := false
	humanOptions := clipresent.HumanOptions{Verbose: *verbose}
	if interactiveConfirmation {
		clipresent.PrintLockOnlyResourceSummary(stdout, clipresent.LockOnlyResourcesFrom(readinessPlanning.LockOnly))
		clipresent.PrintRelationActionsWithOptions(stdout, readinessPlanning.Reconciliation.Relations(), humanOptions)
		relationActionsDisclosed = true
		clipresent.PrintRelationOrderActionsWithOptions(stdout, readinessPlanning.Reconciliation.RelationOrders(), humanOptions)
		relationOrdersDisclosed = true
		clipresent.PrintCarrierAdoptionActionsWithOptions(stdout, readinessPlanning.Reconciliation.CarrierAdoptions(), humanOptions)
		clipresent.PrintCarrierAbsenceActionsWithOptions(stdout, readinessPlanning.Reconciliation.CarrierAbsences(), humanOptions)
		carrierAbsencesDisclosed = true
		clipresent.PrintActionPlanWithOptions(stdout, "apply", readinessPlanning.Reconciliation, humanOptions)
		clipresent.PrintDelegateActionsWithOptions(stdout, readinessPlanning.Reconciliation.Delegates(), humanOptions)
		clipresent.PrintDiagnosticsWithOptions(stdout, readinessPlanning.Diagnostics, humanOptions)
		if applyConfirmationRequired(readinessPlanning.CommandResult) {
			confirmed, err := options.confirmation.prompt("apply")
			if err != nil {
				markOutputFailureReported(stdout)
				printApplyFailure(stderr, err, applyFailureConfirmation, applyworkflow.CommandResult{}, *verbose)
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
		clipresent.PrintRelationOrderActionsWithOptions(stdout, readinessPlanning.Reconciliation.RelationOrders(), humanOptions)
		relationOrdersDisclosed = true
		clipresent.PrintCarrierAdoptionActionsWithOptions(stdout, readinessPlanning.Reconciliation.CarrierAdoptions(), humanOptions)
		clipresent.PrintCarrierAbsenceActionsWithOptions(stdout, readinessPlanning.Reconciliation.CarrierAbsences(), humanOptions)
		carrierAbsencesDisclosed = true
		clipresent.PrintDelegateActionsWithOptions(stdout, readinessPlanning.Reconciliation.Delegates(), humanOptions)
	}

	progress := newApplyProgressRenderer(*jsonOutput, stderr, options)
	executeOptions := options.applyExecuteOptions
	executeOptions.ExecuteEvents = progress.Sink()
	executeOptions.PlanWasDisclosed = interactiveConfirmation
	executeOptions.RelationOrderRiskAuthorizer = nil
	if interactiveConfirmation {
		executeOptions.RelationOrderRiskAuthorizer = newRelationOrderRiskAuthorizer(
			stdout,
			options.confirmation,
			progress.Close,
			humanOptions,
		)
	}
	result, err := applyworkflow.ExecuteWithOptions(options.context, readinessPlanning, executeOptions)
	progress.Close()
	delegateAttemptInputs := clipresent.DelegateAttemptInputsFrom(result.DelegateAttempts)
	if err != nil {
		if *jsonOutput {
			failure := applyworkflow.ClassifyFailure(err, result)
			jsonInput := clipresent.ApplyResultJSONInput{
				ActionCount:            result.ActionCount,
				StatefilePath:          result.StatefilePath,
				LockOnly:               clipresent.LockOnlyResourcesFrom(result.LockOnly),
				Reconciliation:         result.Reconciliation,
				ExecutionAttempted:     result.ExecutionAttempted,
				CarrierAdoptionResults: result.CarrierAdoptionResults,
				RelationOrderResults:   result.RelationOrderResults,
				HostRouteAttempts:      result.HostRouteAttempts,
				MCPStatuses:            mcpStatuses,
				Diagnostics:            result.Diagnostics,
				Failure:                &failure,
			}
			jsonInput.DelegateAttempts = delegateAttemptInputs
			if jsonErr := clipresent.PrintApplyResultJSON(stdout, jsonInput); jsonErr != nil {
				markOutputFailureReported(stdout)
				printApplyFailure(stderr, jsonErr, applyFailureOutput, result, false)
				return 1
			}
			return 1
		}
		clipresent.PrintDelegateAttemptsWithOptions(stderr, delegateAttemptInputs, humanOptions)
		clipresent.PrintCarrierAdoptionResultsWithOptions(
			stderr,
			result.Reconciliation.CarrierAdoptions(),
			result.CarrierAdoptionResults,
			err,
			result.ExecutionAttempted,
			humanOptions,
		)
		clipresent.PrintRelationOrderResultsWithOptions(stderr, result.RelationOrderResults, humanOptions)
		if presentErr := clipresent.PrintHostRouteAttemptsWithOptions(stderr, result.HostRouteAttempts, humanOptions); presentErr != nil {
			printApplyFailure(stderr, presentErr, applyFailureDiagnostics, result, *verbose)
		}
		printApplyFailure(
			stderr,
			err,
			applyFailureExecution,
			result,
			*verbose,
		)
		return 1
	}

	if *jsonOutput {
		jsonInput := clipresent.ApplyResultJSONInput{
			ActionCount:            result.ActionCount,
			StatefilePath:          result.StatefilePath,
			LockOnly:               clipresent.LockOnlyResourcesFrom(result.LockOnly),
			Reconciliation:         result.Reconciliation,
			ExecutionAttempted:     result.ExecutionAttempted,
			CarrierAdoptionResults: result.CarrierAdoptionResults,
			RelationOrderResults:   result.RelationOrderResults,
			HostRouteAttempts:      result.HostRouteAttempts,
			Diagnostics:            result.Diagnostics,
		}
		jsonInput.DelegateAttempts = delegateAttemptInputs
		if err := clipresent.PrintApplyResultJSON(stdout, jsonInput); err != nil {
			markOutputFailureReported(stdout)
			printApplyFailure(stderr, err, applyFailureOutput, result, false)
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "applied: %d actions\n", result.ActionCount)
	clipresent.PrintCarrierAdoptionResultsWithOptions(
		stdout,
		result.Reconciliation.CarrierAdoptions(),
		result.CarrierAdoptionResults,
		nil,
		result.ExecutionAttempted,
		humanOptions,
	)
	clipresent.PrintRelationOrderResultsWithOptions(stdout, result.RelationOrderResults, humanOptions)
	clipresent.PrintPlanResultWithOptions(stdout, result.Reconciliation, humanOptions)
	if !relationActionsDisclosed {
		clipresent.PrintRelationActionsWithOptions(stdout, result.Reconciliation.Relations(), humanOptions)
	}
	if !relationOrdersDisclosed {
		clipresent.PrintRelationOrderActionsWithOptions(stdout, result.Reconciliation.RelationOrders(), humanOptions)
	}
	if !carrierAbsencesDisclosed {
		clipresent.PrintCarrierAbsenceActionsWithOptions(stdout, result.Reconciliation.CarrierAbsences(), humanOptions)
	}
	if err := clipresent.PrintHostRouteAttemptsWithOptions(stdout, result.HostRouteAttempts, humanOptions); err != nil {
		printApplyFailure(stderr, err, applyFailureDiagnostics, result, *verbose)
		return 1
	}
	clipresent.PrintDelegateAttemptsWithOptions(stdout, delegateAttemptInputs, humanOptions)
	clipresent.PrintLockOnlyResourceSummary(stdout, clipresent.LockOnlyResourcesFrom(result.LockOnly))
	if *verbose {
		fmt.Fprintf(stdout, "statefile: %s\n", humanDiagnosticText(result.StatefilePath))
	}

	return 0
}

func printApplyFailure(
	output io.Writer,
	err error,
	stage applyFailureStage,
	result applyworkflow.CommandResult,
	verbose bool,
) {
	detail, reason := applyFailureDetail(stage, err, result)
	fmt.Fprintf(output, "apply failed: %s\n", detail)
	switch reason {
	case applyworkflow.FailureReasonRelationOrderRiskExpanded:
		fmt.Fprintln(
			output,
			"next: inspect a fresh dry-run with daem apply --dry-run, then rerun interactively to authorize the updated extension order",
		)
	case applyworkflow.FailureReasonInterruptedApply:
		fmt.Fprintln(
			output,
			"next: run daem recover --dry-run first",
		)
	case applyworkflow.FailureReasonInterruptedApplyFileSetFence:
		fmt.Fprintln(
			output,
			"next: run daem recover --dry-run first; the file-set fence remains after recover and is not cleared by it",
		)
	case applyworkflow.FailureReasonJournalCleanupIncomplete:
		fmt.Fprintln(output, "next: run daem recover --dry-run to finish journal cleanup")
	case applyworkflow.FailureReasonJournalCleanupFileSetFence:
		fmt.Fprintln(
			output,
			"next: run daem recover --dry-run to finish journal cleanup; the file-set fence remains afterward",
		)
	case applyworkflow.FailureReasonInterruptedFileSetTransaction:
		fmt.Fprintln(output, "next: retry the interrupted authoring or unmanage operation before apply")
	case applyworkflow.FailureReasonFileSetEvidenceInvalid:
		fmt.Fprintln(output, "next: preserve and repair the invalid file-set evidence before apply or recover")
	case applyworkflow.FailureReasonAbandonedFileSetResidue:
		fmt.Fprintln(
			output,
			"next: preserve the reported residue for analysis; do not retry apply or delete reserved names by prefix",
		)
	case applyworkflow.FailureReasonFileSetFenceCensusLimit:
		fmt.Fprintln(output, "next: inspect or reduce StateDir entries so the bounded file-set census can complete")
	case applyworkflow.FailureReasonFileSetAccessUnprovable:
		fmt.Fprintln(output, "next: restore StateDir access and identity before apply or recover")
	}
	if !verbose || err == nil {
		return
	}
	evidence := clipresent.BoundedErrorEvidence(
		err,
		maximumVerboseApplyFailureEvidenceRunes,
	)
	if evidence != "" {
		fmt.Fprintf(output, "apply failure evidence: %s\n", clipresent.Quote(evidence))
	}
}

func applyFailureDetail(
	stage applyFailureStage,
	err error,
	result applyworkflow.CommandResult,
) (string, applyworkflow.FailureReason) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		failure := applyworkflow.ClassifyFailure(err, result)
		return failure.Detail(), failure.Reason()
	}
	if stage == applyFailurePlanning || stage == applyFailureExecution {
		failure := applyworkflow.ClassifyFailure(err, result)
		return failure.Detail(), failure.Reason()
	}
	switch stage {
	case applyFailureProjection:
		return "apply result projection failed", applyworkflow.FailureReasonApplyRefused
	case applyFailureDiff:
		return "apply diff could not be generated", applyworkflow.FailureReasonApplyRefused
	case applyFailureConfirmation:
		return "apply confirmation failed", applyworkflow.FailureReasonApplyRefused
	case applyFailureDiagnostics:
		return "apply diagnostics could not be rendered", applyworkflow.FailureReasonApplyIncomplete
	case applyFailureOutput:
		return "apply output could not be written", applyworkflow.FailureReasonApplyIncomplete
	default:
		return "apply was refused before effects", applyworkflow.FailureReasonApplyRefused
	}
}

func newRelationOrderRiskAuthorizer(
	output io.Writer,
	confirmation confirmationBoundary,
	closeProgress func(),
	humanOptions clipresent.HumanOptions,
) applyworkflow.RelationOrderRiskAuthorizer {
	return func(
		_ context.Context,
		expansion applyworkflow.RelationOrderRiskExpansion,
	) (bool, error) {
		if closeProgress != nil {
			closeProgress()
		}
		fmt.Fprintf(
			output,
			"extension order changed after carrier updates: %d new precedence risks\n",
			expansion.AddedRiskCount(),
		)
		clipresent.PrintRelationOrderRiskDeltasWithOptions(
			output,
			expansion.Deltas(),
			humanOptions,
		)
		return confirmation.prompt("updated apply plan")
	}
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
	for _, decision := range planning.Reconciliation.RelationOrders() {
		if decision.RequiresMutation() {
			return true
		}
	}
	for _, action := range planning.Reconciliation.CarrierAdoptions() {
		if action.StateOnly() {
			return true
		}
	}
	for _, action := range planning.Reconciliation.CarrierAbsences() {
		if action.RequiresConfirmation() {
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

func printApplyWorkflowHints(
	output io.Writer,
	manifestPath string,
	result applyworkflow.CommandResult,
	err error,
	verbose bool,
) {
	printApplyMissingManifestInitHint(output, manifestPath, err, verbose)
	if errors.Is(err, targetselection.ErrInvalid) {
		if verbose {
			printTargetSelectionHint(output, result.ManifestPath)
		} else {
			fmt.Fprintln(output, "next: run daem lock --manifest <manifest> --dry-run")
		}
	}
	if errors.Is(err, applyworkflow.ErrReadLockfile) {
		printApplyLockCommandHint(output, result.ManifestPath, verbose)
	}
	if errors.Is(err, applyworkflow.ErrRelationOrderRiskExpansion) {
		fmt.Fprintln(output, "next: inspect a fresh dry-run with daem apply --dry-run, then rerun interactively to authorize the updated extension order")
	}
}

func printApplyMissingManifestInitHint(
	output io.Writer,
	manifestPath string,
	err error,
	verbose bool,
) {
	resolvedPath, ok := missingManifestInitHintPath(manifestPath, err)
	if !ok {
		return
	}
	if verbose {
		clipresent.PrintShellCommand(output, "next: run ", "daem", "init", "--manifest", resolvedPath, "--dry-run")
		return
	}
	fmt.Fprintln(output, "next: run daem init --manifest <manifest> --dry-run")
}

func printApplyLockCommandHint(output io.Writer, manifestPath string, verbose bool) {
	if verbose {
		printLockCommandHint(output, manifestPath)
		return
	}
	fmt.Fprintln(output, "next: run daem lock --manifest <manifest>")
}
