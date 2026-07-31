package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
)

func runRecover(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"recover"}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"recover"}, stderr)

	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "classify recovery or cleanup evidence without writing")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output with --dry-run")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	yes := flags.Bool("yes", false, "execute recovery without an interactive prompt")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if *dryRun && *yes {
		fmt.Fprintln(stderr, "recover failed: --dry-run and --yes are mutually exclusive")
		return 2
	}
	if err := validatePresentationFlags("recover", *jsonOutput, *verbose, false); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}
	if *jsonOutput && !*dryRun && !*yes {
		fmt.Fprintln(stderr, "recover failed: --json requires --dry-run or --yes")
		return 2
	}
	interactiveConfirmation := !*dryRun && !*yes
	if interactiveConfirmation && !options.confirmation.allowsInteractiveAuthorization() {
		printInteractiveConfirmationRequired(stderr, "recover", "recovery")
		fmt.Fprintln(stderr, "next: run daem recover --dry-run first, then rerun with --yes when the plan is acceptable")
		return 2
	}

	prepared, err := recoverworkflow.Plan(options.context, recoverworkflow.PlanInput{ManifestPath: *manifestPath})
	if err != nil {
		fmt.Fprintf(stderr, "recover failed: %s\n", humanDiagnosticError(err))
		return 1
	}
	defer prepared.Close()
	plan := prepared.Disclosure()

	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintRecoverResultJSON(stdout, "dry-run", plan, nil); err != nil {
				fmt.Fprintf(stderr, "recover failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			if plan.HasErrors() {
				return 1
			}
			return 0
		}
		clipresent.PrintRecoverPlanWithOptions(stdout, plan, clipresent.HumanOptions{Verbose: *verbose})
		if plan.HasErrors() {
			return 1
		}
		return 0
	}

	if plan.Blocked() {
		if *jsonOutput {
			if err := clipresent.PrintRecoverResultJSON(stdout, "write", plan, nil); err != nil {
				fmt.Fprintf(stderr, "recover failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
		}
		if !*jsonOutput {
			clipresent.PrintRecoverPlanWithOptions(stdout, plan, clipresent.HumanOptions{Verbose: *verbose})
		}
		return 1
	}

	if !*jsonOutput {
		clipresent.PrintRecoverPlanWithOptions(stdout, plan, clipresent.HumanOptions{Verbose: *verbose})
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
		executeErr := recoverworkflow.Execute(options.context, prepared)
		if *jsonOutput {
			if err := clipresent.PrintRecoverResultJSON(stdout, "write", plan, executeErr); err != nil {
				fmt.Fprintf(stderr, "recover failed: write json: %s\n", humanDiagnosticError(err))
				return 1
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
				humanDiagnosticError(clipresent.RecoverResultError(plan, executeErr)),
			)
			return 1
		}
		fmt.Fprintln(stdout, "recovery completed")
	}

	return 0
}
