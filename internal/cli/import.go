package cli

import (
	"context"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

type scopeFlagValues []string

func (values *scopeFlagValues) String() string {
	return fmt.Sprint([]string(*values))
}

func (values *scopeFlagValues) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runImport(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
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

	commandPlan, err := workflowadopt.BuildCommandPlan(ctx, workflowadopt.CommandInput{
		TargetValues: targetValues.strings(),
		ScopeValues:  []string(scopeValues),
		ManifestPath: *manifestPath,
		SourceDir:    *sourceDir,
		Merge:        *merge,
	})
	if err != nil {
		fmt.Fprintf(stderr, "import failed: %s\n", humanDiagnosticError(err))
		if commandPlan.OutputPath() == "" {
			return 2
		}
		if workflowadopt.IsNothingToImport(err) {
			printImportNothingToImportHint(stderr, commandPlan.OutputPath(), commandPlan.Merge())
		}
		return 1
	}
	plan := commandPlan.AdoptionPlan()

	if *dryRun {
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
		fmt.Fprintf(stderr, "import failed: %s\n", humanDiagnosticError(err))
		return 1
	}

	commandPlan, err = workflowadopt.ExecuteCommandPlan(ctx, commandPlan)
	if err != nil {
		fmt.Fprintf(stderr, "import failed: %s\n", humanDiagnosticError(err))
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
