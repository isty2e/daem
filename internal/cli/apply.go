package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"

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

func prepareApplyInvocation(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"apply"}, stderr)

	var targetValues targetFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "compute and print actions without writing")
	showDiff := flags.Bool("diff", false, "show file content diffs with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	yes := flags.Bool("yes", false, "bypass confirmation when applying mutations")
	manageExisting := flags.Bool("manage-existing", false, "record exact-match unmanaged outputs as managed without writing those outputs")
	flags.Var(&targetValues, "target", "target to reconcile; may be repeated")
	arguments := prepareGoArguments(flags, args)
	response := newCommandResponse("apply", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"apply"}, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"apply"}, err))
		}

		if flags.NArg() != 0 {
			return response.failure(usageFailure, unexpectedArgumentDiagnostic(flags.Arg(0), targetValues))
		}

		if *dryRun && *yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("apply failed: --dry-run and --yes are mutually exclusive"))
		}
		if *showDiff && !*dryRun {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("apply failed: --diff requires --dry-run"))
		}
		if err := validatePresentationFlags("apply", arguments.output == requestedJSON, *verbose, *showDiff); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		if arguments.output == requestedJSON && !*dryRun && !*yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("apply failed: --json requires --dry-run or --yes"))
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
				return response.failure(operationFailure, applyFailureDiagnostic(*manifestPath, err, applyFailurePlanning, planning.CommandResult, *verbose).WithGuidance(applyWorkflowGuidance(*manifestPath, planning.CommandResult, err)...))
			}
			if err := response.selectOrdinaryResult(); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err))
			}
			mcpStatuses, err := clipresent.MCPStatusesFrom(planning.MCPProjections)
			if err != nil {
				printApplyFailure(stderr, err, applyFailureProjection, applyworkflow.CommandResult{}, *verbose)
				return 1
			}

			if arguments.output == requestedJSON {
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
				printLockCommandHint(stdout, planning.ManifestPath, *manifestPath)
			}
			clipresent.PrintDiagnosticsWithOptions(stdout, planning.Diagnostics, humanOptions)
			if clipresent.PrintReconciliationGuidance(stdout, planning.Reconciliation) {
				printReconciliationInspectionHint(stdout, planning.ManifestPath, *manifestPath, targetValues, *verbose)
			}
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
			if readinessPlanning.ReconciliationReady {
				if err := response.selectOrdinaryResult(); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err))
				}
			}
			if arguments.output == requestedJSON && readinessPlanning.ReconciliationReady {
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
			if arguments.output == requestedJSON {
				return response.failure(operationFailure, applyFailureDiagnostic(*manifestPath, err, applyFailurePlanning, readinessPlanning.CommandResult, false).WithGuidance(applyWorkflowGuidance(*manifestPath, readinessPlanning.CommandResult, err)...))
			}
			printApplyFailure(stderr, err, applyFailurePlanning, readinessPlanning.CommandResult, *verbose)
			if readinessPlanning.ReconciliationReady {
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
			}
			printApplyWorkflowHints(stderr, *manifestPath, readinessPlanning.CommandResult, err)
			if readinessPlanning.ReconciliationReady && clipresent.PrintReconciliationGuidance(stderr, readinessPlanning.Reconciliation) {
				printReconciliationInspectionHint(stderr, readinessPlanning.ManifestPath, *manifestPath, targetValues, *verbose)
			}
			if readinessPlanning.ReconciliationReady && readinessPlanning.Reconciliation.HasLockReadinessErrors() {
				printLockCommandHint(stderr, readinessPlanning.ManifestPath, *manifestPath)
			}
			return 1
		}
		defer readinessPlanning.Close()
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
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
		if !interactiveConfirmation && arguments.output != requestedJSON {
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

		progress := newApplyProgressRenderer(arguments.output == requestedJSON, stderr, options)
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
			if arguments.output == requestedJSON {
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
			if clipresent.PrintReconciliationFailureGuidance(stderr, result.Reconciliation) {
				printReconciliationInspectionHint(stderr, result.ManifestPath, *manifestPath, targetValues, *verbose)
			}
			return 1
		}

		if arguments.output == requestedJSON {
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

		fmt.Fprintf(stdout, "applied: %s\n", clipresent.CountLabel(result.ActionCount, "action", "actions"))
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
	}}
}

func printApplyFailure(
	output io.Writer,
	err error,
	stage applyFailureStage,
	result applyworkflow.CommandResult,
	verbose bool,
) {
	fmt.Fprintln(output, applyFailureDiagnostic("", err, stage, result, verbose).HumanText())
}

func applyFailureDiagnostic(
	requestedManifestPath string,
	err error,
	stage applyFailureStage,
	result applyworkflow.CommandResult,
	verbose bool,
) clipresent.FailureDiagnostic {
	diagnostic, reason := applyFailurePrimary(stage, err, result)
	diagnostic = diagnostic.WithPrefix("apply failed: ")
	var instruction string
	switch reason {
	case applyworkflow.FailureReasonRelationOrderRiskExpanded:
		instruction = "inspect a fresh dry-run with daem apply --dry-run, then rerun interactively to authorize the updated extension order"
	case applyworkflow.FailureReasonInterruptedApply:
		diagnostic = diagnostic.WithGuidance(applyRecoverGuidance(result.ManifestPath, requestedManifestPath, "first"))
	case applyworkflow.FailureReasonInterruptedApplyFileSetFence:
		diagnostic = diagnostic.WithGuidance(applyRecoverGuidance(result.ManifestPath, requestedManifestPath, "first; the file-set fence remains after recover and is not cleared by it"))
	case applyworkflow.FailureReasonJournalCleanupIncomplete:
		diagnostic = diagnostic.WithGuidance(applyRecoverGuidance(result.ManifestPath, requestedManifestPath, "to finish journal cleanup"))
	case applyworkflow.FailureReasonJournalCleanupFileSetFence:
		diagnostic = diagnostic.WithGuidance(applyRecoverGuidance(result.ManifestPath, requestedManifestPath, "to finish journal cleanup; the file-set fence remains afterward"))
	case applyworkflow.FailureReasonInterruptedFileSetTransaction:
		instruction = "retry the interrupted authoring or unmanage operation before apply"
	case applyworkflow.FailureReasonFileSetEvidenceInvalid:
		instruction = "preserve and repair the invalid file-set evidence before apply or recover"
	case applyworkflow.FailureReasonAbandonedFileSetResidue:
		instruction = "preserve the reported residue for analysis; do not retry apply or delete reserved names by prefix"
	case applyworkflow.FailureReasonFileSetFenceCensusLimit:
		instruction = "inspect or reduce StateDir entries so the bounded file-set census can complete"
	case applyworkflow.FailureReasonFileSetAccessUnprovable:
		instruction = "restore StateDir access and identity before apply or recover"
	case applyworkflow.FailureReasonMCPBackendTransition:
		instruction = applyworkflow.ClassifyFailure(err, result).MCPBackendRepairInstruction()
	}
	if instruction != "" {
		diagnostic = diagnostic.WithGuidance(clipresent.InstructionGuidance(instruction))
	}
	if verbose && err != nil {
		diagnostic = diagnostic.WithGuidance(clipresent.BoundedEvidenceGuidance(err, maximumVerboseApplyFailureEvidenceRunes, "apply failure evidence: "))
	}
	return diagnostic
}

func applyRecoverGuidance(manifestPath, requestedManifestPath, purpose string) clipresent.Guidance {
	if manifestPath == "" {
		manifestPath = requestedManifestPath
	}
	if manifestPath != "" {
		argv := clipresent.ManifestCommandArguments([]string{"recover"}, manifestPath, manifestHintOptions(requestedManifestPath, false), "--dry-run")
		// Recovery-barrier diagnostics are path-neutral. A selector that needs
		// an explicit local path remains manual advice, not disclosed argv.
		if !slices.Contains(argv, "--manifest") {
			if guidance, err := clipresent.CommandGuidance(argv, purpose); err == nil {
				return guidance
			}
		}
	}
	return clipresent.InstructionGuidance("run daem recover --dry-run " + purpose)
}

func applyFailurePrimary(stage applyFailureStage, err error, result applyworkflow.CommandResult) (clipresent.FailureDiagnostic, applyworkflow.FailureReason) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		failure := applyworkflow.ClassifyFailure(err, result)
		return clipresent.NewFailureDiagnostic(failure.PrimaryDetail()), failure.Reason()
	}
	if stage == applyFailurePlanning || stage == applyFailureExecution {
		failure := applyworkflow.ClassifyFailure(err, result)
		if stage == applyFailurePlanning {
			if cause, ok := applyInputErrorCause(err, failure); ok {
				return clipresent.FailureDiagnosticFromError(cause), failure.Reason()
			}
		}
		return clipresent.NewFailureDiagnostic(failure.PrimaryDetail()), failure.Reason()
	}
	switch stage {
	case applyFailureProjection:
		return clipresent.NewFailureDiagnostic("apply result projection failed"), applyworkflow.FailureReasonApplyRefused
	case applyFailureDiff:
		return clipresent.NewFailureDiagnostic("apply diff could not be generated"), applyworkflow.FailureReasonApplyRefused
	case applyFailureConfirmation:
		return clipresent.NewFailureDiagnostic("apply confirmation failed"), applyworkflow.FailureReasonApplyRefused
	case applyFailureDiagnostics:
		return clipresent.NewFailureDiagnostic("apply diagnostics could not be rendered"), applyworkflow.FailureReasonApplyIncomplete
	case applyFailureOutput:
		return clipresent.NewFailureDiagnostic("apply output could not be written"), applyworkflow.FailureReasonApplyIncomplete
	default:
		return clipresent.NewFailureDiagnostic("apply was refused before effects"), applyworkflow.FailureReasonApplyRefused
	}
}

func applyInputErrorCause(err error, failure applyworkflow.Failure) (error, bool) {
	if failure.Phase() != applyworkflow.FailurePhasePreflight {
		return nil, false
	}
	switch failure.Reason() {
	case applyworkflow.FailureReasonApplyRefused, applyworkflow.FailureReasonLockfileUnavailable:
		return applyworkflow.InputErrorCause(err)
	default:
		return nil, false
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
			"extension order changed after carrier updates: %s\n",
			clipresent.CountLabel(expansion.AddedRiskCount(), "new precedence risk", "new precedence risks"),
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
) {
	for _, guidance := range applyWorkflowGuidance(manifestPath, result, err) {
		fmt.Fprintln(output, guidance.HumanText())
	}
}

func applyWorkflowGuidance(manifestPath string, result applyworkflow.CommandResult, err error) []clipresent.Guidance {
	var guidance []clipresent.Guidance
	selectedManifest := result.ManifestPath
	if selectedManifest == "" {
		selectedManifest = manifestPath
	}
	failure := applyworkflow.ClassifyFailure(err, result)
	if cause, ok := applyInputErrorCause(err, failure); ok {
		if _, recognized := clipresent.RecognizedInputDiagnostic(cause); !recognized {
			switch {
			case errors.Is(cause, applyworkflow.ErrReadLockfile) && errors.Is(cause, fs.ErrNotExist):
				guidance = append(guidance, manifestCommandGuidance([]string{"lock"}, selectedManifest, manifestHintOptions(manifestPath, false)))
			case errors.Is(cause, fs.ErrNotExist):
				if init, present := missingManifestInitGuidance(selectedManifest, cause); present {
					guidance = append(guidance, init)
				}
			default:
				guidance = append(guidance, clipresent.InstructionGuidance("correct the reported manifest or lockfile input, then retry"))
			}
		}
	}
	if failure.Reason() == applyworkflow.FailureReasonApplyRefused && errors.Is(err, targetselection.ErrInvalid) {
		guidance = append(guidance, targetSelectionGuidance(selectedManifest, manifestPath)...)
	}
	return guidance
}
