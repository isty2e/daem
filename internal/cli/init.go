package cli

import (
	"context"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	initworkflow "github.com/isty2e/daem/internal/workflow/init"
)

type initRequest struct {
	manifestPath string
	force        bool
	dryRun       bool
}

func runInit(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"init"}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"init"}, stderr)

	manifestPath := flags.String("manifest", "", "path to daem.toml")
	force := flags.Bool("force", false, "overwrite an existing manifest")
	dryRun := flags.Bool("dry-run", false, "preview manifest creation without writing")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if err := validatePresentationFlags("init", *jsonOutput, *verbose, false); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "init failed: %s\n", humanDiagnosticError(err))
		return 1
	}

	request := initRequest{
		manifestPath: *manifestPath,
		force:        *force,
		dryRun:       *dryRun,
	}
	input := initworkflow.Input{
		ManifestPath: request.manifestPath,
		Force:        request.force,
	}
	if request.dryRun {
		plan, err := initworkflow.BuildPlan(ctx, input)
		if err != nil {
			fmt.Fprintf(stderr, "init failed: %s\n", humanDiagnosticError(err))
			return 1
		}
		if *jsonOutput {
			if err := clipresent.PrintInitJSON(stdout, "dry-run", plan); err != nil {
				fmt.Fprintf(stderr, "init failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintInitPlanWithOptions(stdout, "init", plan, clipresent.HumanOptions{Verbose: *verbose})
		return 0
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "init failed: %s\n", humanDiagnosticError(err))
		return 1
	}
	plan, err := initworkflow.Execute(ctx, input)
	if err != nil {
		fmt.Fprintf(stderr, "init failed: write manifest: %s\n", humanDiagnosticError(err))
		return 1
	}
	if plan.Action == initworkflow.ActionOverwrite {
		if *jsonOutput {
			if err := clipresent.PrintInitJSON(stdout, "write", plan); err != nil {
				fmt.Fprintf(stderr, "init failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintInitPlanWithOptions(stdout, "overwritten", plan, clipresent.HumanOptions{Verbose: *verbose})
		return 0
	}
	if *jsonOutput {
		if err := clipresent.PrintInitJSON(stdout, "write", plan); err != nil {
			fmt.Fprintf(stderr, "init failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintInitPlanWithOptions(stdout, "created", plan, clipresent.HumanOptions{Verbose: *verbose})
	return 0
}
