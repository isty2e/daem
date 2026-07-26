package cli

import (
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	listworkflow "github.com/isty2e/daem/internal/workflow/list"
	statusworkflow "github.com/isty2e/daem/internal/workflow/status"
)

func runList(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	if handled, exitCode := handleGroupHelp("list", args, stdout, stderr); handled {
		return exitCode
	}
	subcommand := args[0]
	if subcommand != "resources" && subcommand != "outputs" {
		fmt.Fprintf(stderr, "unknown list resource %q\n", subcommand)
		fmt.Fprintln(stderr, "next: run daem help list")
		return 2
	}
	if commandHelpRequested(args[1:]) {
		printCommandUsage([]string{"list", subcommand}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"list", subcommand}, stderr)
	var targetValues targetFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "target to enumerate; may be repeated")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if err := validatePresentationFlags("list", *jsonOutput, *verbose, false); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}
	targets := targetValues.strings()
	if err := options.context.Err(); err != nil {
		fmt.Fprintf(stderr, "list failed: %s\n", humanDiagnosticError(err))
		return 1
	}
	if subcommand == "outputs" {
		result, err := statusworkflow.Run(options.context, statusworkflow.CommandInput{
			ManifestPath: *manifestPath,
			TargetValues: targets,
		})
		if err != nil {
			fmt.Fprintf(stderr, "list failed: %s\n", humanDiagnosticError(err))
			printMissingManifestInitHint(stderr, *manifestPath, err)
			if errors.Is(err, targetselection.ErrInvalid) {
				printTargetSelectionHint(stderr, result.ManifestPath)
			}
			return 1
		}
		if *jsonOutput {
			if err := clipresent.PrintListOutputsJSON(stdout, result.Inventory); err != nil {
				fmt.Fprintf(stderr, "list failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintInventoryReportWithOptions(stdout, result.Inventory, clipresent.HumanOptions{Verbose: *verbose})
		return 0
	}

	result, err := listworkflow.Run(options.context, listworkflow.Input{
		ManifestPath: *manifestPath,
		TargetValues: targets,
	})
	if err != nil {
		fmt.Fprintf(stderr, "list failed: %s\n", humanDiagnosticError(err))
		printMissingManifestInitHint(stderr, result.ManifestPath, err)
		if errors.Is(err, targetselection.ErrInvalid) {
			printTargetSelectionHint(stderr, result.ManifestPath)
		}
		return 1
	}
	rows := clipresent.ListRows(result.Environment, result.SkillGroups(), result.Selection)
	if *jsonOutput {
		if err := clipresent.PrintListResourcesJSON(stdout, result.ManifestPath, rows); err != nil {
			fmt.Fprintf(stderr, "list failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintListRowsWithOptions(stdout, result.ManifestPath, rows, clipresent.HumanOptions{Verbose: *verbose})
	return 0
}
