package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

func runImport(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	return prepareImportInvocation(args, stdout, stderr, options).execute()
}

func prepareImportInvocation(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"import"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "manifest to create or merge")
	sourceDir := flags.String("source-dir", "", "directory for imported instruction source files")
	dryRun := flags.Bool("dry-run", false, "preview import without writing")
	showDiff := flags.Bool("diff", false, "show generated manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	merge := flags.Bool("merge", false, "merge import result into an existing manifest")
	flags.Var(&targetValues, "target", "target to inspect; may be repeated")
	flags.Var(&scopeValues, "scope", "scope to inspect; may be repeated")
	arguments := prepareGoArguments(flags, args)
	response := newCommandResponse("import", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		ctx := options.context
		if commandHelpRequested(args) {
			printCommandUsage([]string{"import"}, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"import"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, unexpectedArgumentDiagnostic(flags.Arg(0), targetValues))
		}
		if *showDiff && !*dryRun {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("import failed: --diff requires --dry-run"))
		}
		if err := validatePresentationFlags("import", arguments.output == requestedJSON, *verbose, *showDiff); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		if err := ctx.Err(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("import failed: "))
		}

		progress := newImportProgressRenderer(arguments.output == requestedJSON, stderr, options)
		defer progress.Close()

		commandPlan, err := workflowadopt.BuildCommandPlan(ctx, workflowadopt.CommandInput{
			TargetValues:   targetValues.strings(),
			ScopeValues:    scopeValues.strings(),
			ManifestPath:   *manifestPath,
			SourceDir:      *sourceDir,
			Merge:          *merge,
			ProgressEvents: progress.Sink(),
		})
		if err != nil {
			progress.Close()
			if commandPlan.OutputPath() == "" {
				return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("import failed: "))
			}
			return response.failure(operationFailure, importFailureDiagnostic(err, commandPlan, *verbose))
		}
		plan := commandPlan.AdoptionPlan()

		if *dryRun {
			progress.Close()
			if err := response.selectOrdinaryResult(); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err))
			}
			if arguments.output == requestedJSON {
				if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ImportPlanJSONOutput("dry-run", plan)); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("import failed: write json: "))
				}
				if commandPlan.HasMergeConflicts() {
					return 1
				}
				return 0
			}
			clipresent.PrintImportPlanWithOptions(stdout, clipresent.ImportPlanFromAdoption("import", plan, true), clipresent.HumanOptions{Verbose: *verbose})
			if commandPlan.HasMergeConflicts() {
				return 1
			}
			if *showDiff {
				beforePath, beforeContent, afterPath, afterContent := commandPlan.ManifestDiff()
				clipresent.PrintManifestDiff(stdout, beforePath, beforeContent, afterPath, afterContent)
			}
			return 0
		}
		if commandPlan.HasMergeConflicts() {
			progress.Close()
			if err := response.selectOrdinaryResult(); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err))
			}
			if arguments.output == requestedJSON {
				if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ImportPlanJSONOutput("write", plan)); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("import failed: write json: "))
				}
				return 1
			}
			clipresent.PrintImportSkippedReport(
				stderr,
				plan.Skipped(),
				clipresent.HumanOptions{Verbose: *verbose},
				false,
			)
			fmt.Fprintln(stderr, "import failed: merge conflicts detected; run daem import --merge --dry-run to inspect conflicts")
			return 1
		}
		if err := ctx.Err(); err != nil {
			progress.Close()
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("import failed: "))
		}

		optimisticPlan := commandPlan
		commandPlan, err = workflowadopt.ExecuteCommandPlan(ctx, commandPlan, progress.Sink())
		progress.Close()
		if err != nil {
			return response.failure(operationFailure, importFailureDiagnostic(err, optimisticPlan, *verbose))
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		plan = commandPlan.AdoptionPlan()
		if arguments.output == requestedJSON {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ImportPlanJSONOutput("write", plan)); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("import failed: write json: "))
			}
			return 0
		}
		clipresent.PrintImportPlanWithOptions(stdout, clipresent.ImportPlanFromAdoption("imported", plan, false), manifestHintOptions(*manifestPath, *verbose))
		return 0
	}}
}

func importFailureDiagnostic(
	err error,
	commandPlan workflowadopt.CommandPlan,
	verbose bool,
) clipresent.FailureDiagnostic {
	diagnostic := humanFailureDiagnostic(err).WithPrefix("import failed: ")
	skipped, overflow := workflowadopt.ImportFailureSkipped(err)
	diagnostic = diagnostic.WithGuidance(clipresent.ImportFailureSkippedGuidance(skipped, clipresent.HumanOptions{Verbose: verbose}, overflow)...)
	if workflowadopt.IsNothingToImport(err) {
		diagnostic = diagnostic.WithGuidance(importNothingToImportGuidance(commandPlan.OutputPath(), commandPlan.Merge())...)
	}
	return diagnostic
}
