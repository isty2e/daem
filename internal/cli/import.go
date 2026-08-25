package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

func runImport(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	ctx := options.context
	if commandHelpRequested(args) {
		printCommandUsage([]string{"import"}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"import"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "manifest to create or merge")
	sourceDir := flags.String("source-dir", "", "directory for imported instruction source files")
	dryRun := flags.Bool("dry-run", false, "preview import without writing")
	showDiff := flags.Bool("diff", false, "show generated manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	merge := flags.Bool("merge", false, "merge import result into an existing manifest")
	flags.Var(&targetValues, "target", "target to inspect; may be repeated")
	flags.Var(&scopeValues, "scope", "scope to inspect; may be repeated")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		printUnexpectedArgumentWithTargetHint(stderr, flags.Arg(0), targetValues)
		return 2
	}
	if *showDiff && !*dryRun {
		fmt.Fprintln(stderr, "import failed: --diff requires --dry-run")
		return 2
	}
	if err := validatePresentationFlags("import", *jsonOutput, *verbose, *showDiff); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "import failed: %s\n", humanDiagnosticError(err))
		return 1
	}

	progress := newImportProgressRenderer(*jsonOutput, stderr, options)
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
		fmt.Fprintf(stderr, "import failed: %s\n", humanDiagnosticError(err))
		if commandPlan.OutputPath() == "" {
			return 2
		}
		printImportFailureDetails(stderr, err, commandPlan, *verbose)
		return 1
	}
	plan := commandPlan.AdoptionPlan()

	if *dryRun {
		progress.Close()
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ImportPlanJSONOutput("dry-run", plan)); err != nil {
				fmt.Fprintf(stderr, "import failed: write json: %s\n", humanDiagnosticError(err))
				return 1
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
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ImportPlanJSONOutput("write", plan)); err != nil {
				fmt.Fprintf(stderr, "import failed: write json: %s\n", humanDiagnosticError(err))
			}
			return 1
		}
		fmt.Fprintln(stderr, "import failed: merge conflicts detected; run daem import --merge --dry-run to inspect conflicts")
		return 1
	}
	if err := ctx.Err(); err != nil {
		progress.Close()
		fmt.Fprintf(stderr, "import failed: %s\n", humanDiagnosticError(err))
		return 1
	}

	optimisticPlan := commandPlan
	commandPlan, err = workflowadopt.ExecuteCommandPlan(ctx, commandPlan, progress.Sink())
	progress.Close()
	if err != nil {
		fmt.Fprintf(stderr, "import failed: %s\n", humanDiagnosticError(err))
		printImportFailureDetails(stderr, err, optimisticPlan, *verbose)
		return 1
	}
	plan = commandPlan.AdoptionPlan()
	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ImportPlanJSONOutput("write", plan)); err != nil {
			fmt.Fprintf(stderr, "import failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintImportPlanWithOptions(stdout, clipresent.ImportPlanFromAdoption("imported", plan, false), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func printImportFailureDetails(
	output io.Writer,
	err error,
	commandPlan workflowadopt.CommandPlan,
	verbose bool,
) {
	skipped, overflow := workflowadopt.ImportFailureSkipped(err)
	if len(skipped) != 0 || overflow {
		clipresent.PrintImportSkippedReport(
			output,
			skipped,
			clipresent.HumanOptions{Verbose: verbose},
			overflow,
		)
	}
	if workflowadopt.IsNothingToImport(err) {
		printImportNothingToImportHint(output, commandPlan.OutputPath(), commandPlan.Merge())
	}
}
