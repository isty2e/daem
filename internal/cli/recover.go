package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
)

func prepareRecoverInvocation(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"recover"}, stderr)

	manifestPath := flags.String("manifest", "", "path to daem.toml")
	legacyUserState := flags.Bool("legacy-user-state", false, "recover the default user manifest's former local state")
	dryRun := flags.Bool("dry-run", false, "classify recovery or cleanup evidence without writing")
	flags.Bool("json", false, "emit structured JSON output with --dry-run")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	yes := flags.Bool("yes", false, "execute recovery without an interactive prompt")
	arguments := prepareGoArguments(flags, args)
	response := newCommandResponse("recover", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"recover"}, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"recover"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()))
		}
		if *dryRun && *yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("recover failed: --dry-run and --yes are mutually exclusive"))
		}
		if err := validatePresentationFlags("recover", arguments.output == requestedJSON, *verbose, false); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		if arguments.output == requestedJSON && !*dryRun && !*yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("recover failed: --json requires --dry-run or --yes"))
		}
		interactiveConfirmation := !*dryRun && !*yes
		if interactiveConfirmation && !options.confirmation.allowsInteractiveAuthorization() {
			printInteractiveConfirmationRequired(stderr, "recover", "recovery")
			fmt.Fprintln(stderr, "next: run daem recover --dry-run first, then rerun with --yes when the plan is acceptable")
			return 2
		}

		prepared, err := recoverworkflow.Plan(options.context, recoverworkflow.PlanInput{ManifestPath: *manifestPath, LegacyUserState: *legacyUserState})
		if err != nil {
			return response.failure(operationFailure, clipresent.RecoverPlanningDiagnostic(err, clipresent.HumanOptions{Verbose: *verbose}))
		}
		defer prepared.Close()
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		plan := prepared.Disclosure()
		fileSetFence, _ := prepared.ContinuingFileSetFence()

		if *dryRun {
			if arguments.output == requestedJSON {
				if err := clipresent.PrintRecoverResultJSONWithFence(stdout, "dry-run", plan, fileSetFence, nil, nil); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("recover failed: write json: "))
				}
				if plan.HasErrors() {
					return 1
				}
				return 0
			}
			clipresent.PrintRecoverPlanWithFenceOptions(stdout, plan, fileSetFence, clipresent.HumanOptions{Verbose: *verbose})
			if plan.HasErrors() {
				return 1
			}
			return 0
		}

		if plan.Blocked() || plan.HasErrors() {
			if arguments.output == requestedJSON {
				if err := clipresent.PrintRecoverResultJSONWithFence(stdout, "write", plan, fileSetFence, nil, nil); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("recover failed: write json: "))
				}
			}
			if arguments.output != requestedJSON {
				clipresent.PrintRecoverPlanWithFenceOptions(stdout, plan, fileSetFence, clipresent.HumanOptions{Verbose: *verbose})
			}
			return 1
		}

		if arguments.output != requestedJSON {
			clipresent.PrintRecoverPlanWithFenceOptions(stdout, plan, fileSetFence, clipresent.HumanOptions{Verbose: *verbose})
		}
		if interactiveConfirmation {
			confirmed, err := options.confirmation.prompt("recover")
			if err != nil {
				printConfirmationFailure(stderr, "recover", err)
				return 1
			}
			if !confirmed {
				fmt.Fprintln(stderr, "recover canceled")
				return 1
			}
		}
		if *yes || interactiveConfirmation {
			executionResult, executeErr := recoverworkflow.Execute(
				options.context,
				prepared,
				options.recoverExecuteOptions,
			)
			if arguments.output == requestedJSON {
				if err := clipresent.PrintRecoverResultJSONWithFence(
					stdout,
					"write",
					plan,
					fileSetFence,
					&executionResult,
					executeErr,
				); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("recover failed: write json: "))
				}
				if executeErr != nil {
					return 1
				}
				return 0
			}
			if executeErr != nil {
				fmt.Fprintf(
					stderr,
					"recover failed: %s\n",
					humanDiagnosticError(clipresent.RecoverResultError(plan, &executionResult, executeErr)),
				)
				return 1
			}
			if executionResult.HasNonClearFileSetObservation() {
				clipresent.PrintRecoverExecutionFence(stdout, executionResult)
			} else {
				fmt.Fprintln(stdout, "recovery completed")
			}
		}

		return 0
	}}
}
