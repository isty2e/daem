package cli

import (
	"fmt"
	"io"
	"time"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

func runRefresh(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	options commandOptions,
) int {
	if handled, exitCode := handleGroupHelp(
		"refresh",
		args,
		stdout,
		stderr,
	); handled {
		return exitCode
	}
	if args[0] != "extension" {
		fmt.Fprintf(
			stderr,
			"refresh failed: unknown refresh subject %q\n",
			args[0],
		)
		fmt.Fprintln(stderr, "next: run daem help refresh")
		return 2
	}
	return prepareRefreshExtensionInvocation(args[1:], stdout, stderr, options).execute()
}

func prepareRefreshExtensionInvocation(args []string, stdout, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"refresh", "extension"}, stderr)
	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	timeout := flags.Duration(
		"timeout",
		refreshworkflow.DefaultHostCommandTimeout,
		"delegated host command timeout",
	)
	dryRun := flags.Bool(
		"dry-run",
		false,
		"disclose the exact refresh plan without host effects",
	)
	yes := flags.Bool(
		"yes",
		false,
		"authorize the exact disclosed refresh without a prompt",
	)
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool(
		"verbose",
		false,
		"emit additional human-readable evidence",
	)
	flags.Var(
		&targetValues,
		"target",
		"exact target selector; at most one distinct value",
	)
	flags.Var(
		&scopeValues,
		"scope",
		"exact scope selector; at most one distinct value",
	)
	arguments := prepareNamedArguments(flags, args, "extension id")
	response := newCommandResponse("refresh extension", arguments.options.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"refresh", "extension"}, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			if arguments.missing != nil {
				return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("refresh failed: "))
			}
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"refresh", "extension"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()).WithPrefix("refresh failed: "))
		}
		hostCommandTimeout, err := refreshworkflow.NewHostCommandTimeout(*timeout)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("refresh failed: --timeout: "))
		}
		if *dryRun && *yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("refresh failed: --dry-run and --yes are mutually exclusive"))
		}
		if err := validatePresentationFlags(
			"refresh",
			arguments.options.output == requestedJSON,
			*verbose,
			false,
		); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		if arguments.options.output == requestedJSON && !*dryRun && !*yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("refresh failed: --json requires --dry-run or --yes"))
		}
		targets := targetValues.strings()
		if len(targets) > 1 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("refresh failed: refresh accepts at most one distinct --target"))
		}
		scopeValue, err := singleScopeValue(scopeValues)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("refresh failed: "))
		}
		targetValue := ""
		if len(targets) == 1 {
			targetValue = targets[0]
		}
		input := refreshworkflow.CommandInput{
			ManifestPath: *manifestPath,
			ExtensionID:  arguments.name,
			TargetValue:  targetValue,
			ScopeValue:   scopeValue,
			Timeout:      hostCommandTimeout.Duration(),
		}
		interactiveConfirmation := !*dryRun && !*yes
		if interactiveConfirmation &&
			!options.confirmation.allowsInteractiveAuthorization() {
			printInteractiveConfirmationRequired(stderr, "refresh", "refresh")
			fmt.Fprintln(
				stderr,
				"next: run daem refresh extension <id> --dry-run first, then rerun with --yes when the plan is acceptable",
			)
			return 2
		}

		if *dryRun {
			result, planErr := refreshworkflow.PlanDryRun(
				options.context,
				input,
				options.refreshPlanOptions,
			)
			if err := response.selectOrdinaryResult(); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err))
			}
			if renderErr := printRefreshResult(
				stdout,
				clipresent.RefreshReportFrom(result),
				arguments.options.output == requestedJSON,
				*verbose,
				false,
			); renderErr != nil {
				return 1
			}
			if planErr != nil {
				if arguments.options.output != requestedJSON {
					printRefreshFailure(stderr, result)
					printMissingManifestInitHint(stderr, *manifestPath, planErr)
				}
				return 1
			}
			return refreshExitCode(result)
		}

		prepared, planErr := refreshworkflow.PlanWrite(
			options.context,
			input,
			options.refreshPlanOptions,
		)
		if planErr != nil {
			if prepared != nil {
				defer prepared.Close()
				if err := response.selectOrdinaryResult(); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err))
				}
				if renderErr := printRefreshResult(
					stdout,
					clipresent.RefreshReportFrom(prepared.Disclosure()),
					arguments.options.output == requestedJSON,
					*verbose,
					false,
				); renderErr != nil {
					return 1
				}
			}
			if arguments.options.output != requestedJSON {
				if prepared != nil {
					printRefreshFailure(stderr, prepared.Disclosure())
				} else {
					fmt.Fprintln(stderr, "refresh failed")
				}
				printMissingManifestInitHint(stderr, *manifestPath, planErr)
			} else if prepared == nil {
				diagnostic := clipresent.NewFailureDiagnostic("refresh failed")
				if init, present := missingManifestInitGuidance(*manifestPath, planErr); present {
					diagnostic = diagnostic.WithGuidance(init)
				}
				return response.failure(operationFailure, diagnostic)
			}
			return 1
		}
		defer prepared.Close()
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		disclosed := clipresent.RefreshReportFrom(prepared.Disclosure())
		if arguments.options.output == requestedJSON {
			if err := clipresent.PrintRefreshJSON(stderr, disclosed); err != nil {
				fmt.Fprintf(
					stderr,
					"refresh failed: disclose plan: %s\n",
					humanDiagnosticError(err),
				)
				return 1
			}
		} else {
			clipresent.PrintRefreshReport(
				stdout,
				disclosed,
				clipresent.HumanOptions{Verbose: *verbose},
			)
			if options.confirmation.disclosureError != nil {
				if err := options.confirmation.disclosureError(); err != nil {
					fmt.Fprintf(
						stderr,
						"refresh failed: disclose plan: %s\n",
						humanDiagnosticError(err),
					)
					return 1
				}
			}
		}

		if interactiveConfirmation {
			confirmed, confirmErr := options.confirmation.prompt("refresh")
			if confirmErr != nil {
				printConfirmationFailure(stderr, "refresh", confirmErr)
				return 1
			}
			if !confirmed {
				cancelled, cancelErr := refreshworkflow.Cancel(prepared)
				if cancelErr != nil {
					printRefreshFailure(stderr, cancelled)
					return 1
				}
				clipresent.PrintRefreshOutcome(
					stdout,
					clipresent.RefreshReportFrom(cancelled),
					clipresent.HumanOptions{Verbose: *verbose},
				)
				fmt.Fprintln(stderr, "refresh canceled")
				return 1
			}
		}

		progress := newRefreshProgressRenderer(arguments.options.output == requestedJSON, stderr, options)
		progress.Start(
			disclosed.Selection.ID,
			time.Duration(disclosed.Disclosure.TimeoutSeconds)*time.Second,
		)
		result, executeErr := refreshworkflow.Execute(
			options.context,
			prepared,
			options.refreshExecuteOptions,
		)
		progress.Close()
		report := clipresent.RefreshReportFrom(result)
		if renderErr := printRefreshResult(
			stdout,
			report,
			arguments.options.output == requestedJSON,
			*verbose,
			arguments.options.output != requestedJSON,
		); renderErr != nil {
			return 1
		}
		if executeErr != nil && arguments.options.output != requestedJSON {
			printRefreshFailure(stderr, result)
		}
		return refreshExitCode(result)
	}}
}

func printRefreshFailure(output io.Writer, result refreshworkflow.CommandResult) {
	detail := result.FailureDetail()
	if detail == "" {
		fmt.Fprintln(output, "refresh failed")
		return
	}
	fmt.Fprintf(output, "refresh failed: %s\n", detail)
}

func printRefreshResult(
	output io.Writer,
	report clipresent.RefreshReport,
	jsonOutput bool,
	verbose bool,
	outcomeOnly bool,
) error {
	if jsonOutput {
		return clipresent.PrintRefreshJSON(output, report)
	}
	if outcomeOnly {
		clipresent.PrintRefreshOutcome(
			output,
			report,
			clipresent.HumanOptions{Verbose: verbose},
		)
		return nil
	}
	clipresent.PrintRefreshReport(
		output,
		report,
		clipresent.HumanOptions{Verbose: verbose},
	)
	return nil
}

func refreshExitCode(result refreshworkflow.CommandResult) int {
	if !result.HasErrors() {
		return 0
	}
	if result.ProcessOutcome != nil &&
		(result.ProcessOutcome.Cancelled ||
			result.ProcessOutcome.Signaled) &&
		result.ProcessOutcome.ExitCode != nil &&
		(*result.ProcessOutcome.ExitCode == 130 ||
			*result.ProcessOutcome.ExitCode == 143) {
		return *result.ProcessOutcome.ExitCode
	}
	return 1
}
