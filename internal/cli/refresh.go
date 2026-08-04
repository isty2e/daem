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
	if commandHelpRequested(args[1:]) {
		printCommandUsage([]string{"refresh", "extension"}, stdout, 0)
		return 0
	}
	if len(args) < 2 {
		fmt.Fprintln(stderr, "refresh failed: extension id is required")
		return 2
	}

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
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
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

	extensionID := args[1]
	if err := flags.Parse(args[2:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(
			stderr,
			"refresh failed: unexpected argument %q\n",
			flags.Arg(0),
		)
		return 2
	}
	hostCommandTimeout, err := refreshworkflow.NewHostCommandTimeout(*timeout)
	if err != nil {
		fmt.Fprintf(
			stderr,
			"refresh failed: --timeout: %s\n",
			humanDiagnosticError(err),
		)
		return 2
	}
	if *dryRun && *yes {
		fmt.Fprintln(
			stderr,
			"refresh failed: --dry-run and --yes are mutually exclusive",
		)
		return 2
	}
	if err := validatePresentationFlags(
		"refresh",
		*jsonOutput,
		*verbose,
		false,
	); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}
	if *jsonOutput && !*dryRun && !*yes {
		fmt.Fprintln(
			stderr,
			"refresh failed: --json requires --dry-run or --yes",
		)
		return 2
	}
	targets := targetValues.strings()
	if len(targets) > 1 {
		fmt.Fprintln(
			stderr,
			"refresh failed: refresh accepts at most one distinct --target",
		)
		return 2
	}
	scopeValue, err := addScope(scopeValues)
	if err != nil {
		fmt.Fprintf(
			stderr,
			"refresh failed: %s\n",
			humanDiagnosticError(err),
		)
		return 2
	}
	targetValue := ""
	if len(targets) == 1 {
		targetValue = targets[0]
	}
	input := refreshworkflow.CommandInput{
		ManifestPath: *manifestPath,
		ExtensionID:  extensionID,
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
		if renderErr := printRefreshResult(
			stdout,
			clipresent.RefreshReportFrom(result),
			*jsonOutput,
			*verbose,
			false,
		); renderErr != nil {
			return 1
		}
		if planErr != nil {
			if !*jsonOutput {
				fmt.Fprintf(
					stderr,
					"refresh failed: %s\n",
					humanDiagnosticError(planErr),
				)
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
		resultWritten := false
		if prepared != nil {
			defer prepared.Close()
			if renderErr := printRefreshResult(
				stdout,
				clipresent.RefreshReportFrom(prepared.Disclosure()),
				*jsonOutput,
				*verbose,
				false,
			); renderErr != nil {
				return 1
			}
			resultWritten = true
		}
		if !*jsonOutput || !resultWritten {
			fmt.Fprintf(
				stderr,
				"refresh failed: %s\n",
				humanDiagnosticError(planErr),
			)
			printMissingManifestInitHint(stderr, *manifestPath, planErr)
		}
		return 1
	}
	defer prepared.Close()
	disclosed := clipresent.RefreshReportFrom(prepared.Disclosure())
	if *jsonOutput {
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
				fmt.Fprintf(
					stderr,
					"refresh failed: %s\n",
					humanDiagnosticError(cancelErr),
				)
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

	progress := newRefreshProgressRenderer(*jsonOutput, stderr, options)
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
		*jsonOutput,
		*verbose,
		!*jsonOutput,
	); renderErr != nil {
		return 1
	}
	if executeErr != nil && !*jsonOutput {
		fmt.Fprintf(
			stderr,
			"refresh failed: %s\n",
			humanDiagnosticError(executeErr),
		)
	}
	return refreshExitCode(result)
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
