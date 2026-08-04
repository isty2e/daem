package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func runAddInstruction(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"add", "instruction"}, stdout, 0)
		return 0
	}

	name, sourceArg, flagArgs, err := splitAddInstructionArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"add", "instruction"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"add", "instruction"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "target to add; may be repeated")
	flags.Var(&scopeValues, "scope", "scope for the instruction resource")

	if err := flags.Parse(flagArgs); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		printUnexpectedArgumentWithTargetHint(stderr, flags.Arg(0), targetValues)
		return 2
	}
	if *showDiff && !*dryRun {
		fmt.Fprintln(stderr, "add failed: --diff requires --dry-run")
		return 2
	}
	if err := validatePresentationFlags("add", *jsonOutput, *verbose, *showDiff); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}

	targets := targetValues.strings()
	scope, err := singleScopeValue(scopeValues)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		return 2
	}

	result, err := authoring.AddInstruction(ctx, authoringExecutionOptions(*manifestPath, *dryRun), authoring.AddInstructionRequest{
		Name:      name,
		SourceArg: sourceArg,
		Targets:   targets,
		Scope:     scope,
	})
	if err != nil {
		printAuthoringOperationError(stderr, "add", *manifestPath, err)
		return 1
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceInstructions, result)); err != nil {
				fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("add", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceInstructions, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceInstructions, result)); err != nil {
			fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("added", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceInstructions, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func splitAddInstructionArgs(args []string) (string, string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 2, instructionAuthoringFlagTakesValue)
	if err != nil {
		return "", "", nil, err
	}
	if len(positionals) == 0 || strings.TrimSpace(positionals[0]) == "" {
		return "", "", nil, fmt.Errorf("missing instruction name")
	}
	if len(positionals) == 1 || strings.TrimSpace(positionals[1]) == "" {
		return "", "", nil, fmt.Errorf("missing instruction source")
	}
	name, err := authoring.CleanInstructionName(positionals[0])
	if err != nil {
		return "", "", nil, err
	}
	return name, strings.TrimSpace(positionals[1]), flagArgs, nil
}

func instructionAuthoringFlagTakesValue(name string) bool {
	switch name {
	case "manifest", "target", "scope":
		return true
	default:
		return false
	}
}
