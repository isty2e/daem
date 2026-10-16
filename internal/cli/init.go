package cli

import (
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	initworkflow "github.com/isty2e/daem/internal/workflow/init"
)

type initRequest struct {
	manifestPath string
	force        bool
	dryRun       bool
}

func prepareInitInvocation(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"init"}, stderr)

	manifestPath := flags.String("manifest", "", "path to daem.toml")
	force := flags.Bool("force", false, "overwrite an existing manifest")
	dryRun := flags.Bool("dry-run", false, "preview manifest creation without writing")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	arguments := prepareGoArguments(flags, args)
	response := newCommandResponse("init", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		ctx := options.context
		if commandHelpRequested(args) {
			printCommandUsage([]string{"init"}, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"init"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()))
		}
		if err := validatePresentationFlags("init", arguments.output == requestedJSON, *verbose, false); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		if err := ctx.Err(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("init failed: "))
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
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("init failed: "))
			}
			if err := response.selectOrdinaryResult(); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err))
			}
			if arguments.output == requestedJSON {
				if err := clipresent.PrintInitJSON(stdout, "dry-run", plan); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("init failed: write json: "))
				}
				return 0
			}
			clipresent.PrintInitPlanWithOptions(stdout, "init", plan, clipresent.HumanOptions{Verbose: *verbose})
			return 0
		}
		if err := ctx.Err(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("init failed: "))
		}
		plan, err := initworkflow.Execute(ctx, input)
		if err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("init failed: write manifest: "))
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		if plan.Action == initworkflow.ActionOverwrite {
			if arguments.output == requestedJSON {
				if err := clipresent.PrintInitJSON(stdout, "write", plan); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("init failed: write json: "))
				}
				return 0
			}
			clipresent.PrintInitPlanWithOptions(stdout, "overwritten", plan, manifestHintOptions(*manifestPath, *verbose))
			return 0
		}
		if arguments.output == requestedJSON {
			if err := clipresent.PrintInitJSON(stdout, "write", plan); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("init failed: write json: "))
			}
			return 0
		}
		clipresent.PrintInitPlanWithOptions(stdout, "created", plan, manifestHintOptions(*manifestPath, *verbose))
		return 0
	}}
}
