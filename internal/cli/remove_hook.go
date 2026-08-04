package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func runRemoveHook(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"remove", "hook"}, stdout, 0)
		return 0
	}

	resourceName, flagArgs, err := splitRemoveHookArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "remove failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"remove", "hook"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"remove", "hook"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "target to remove; may be repeated")
	flags.Var(&scopeValues, "scope", "scope to remove from")

	if err := flags.Parse(flagArgs); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		printUnexpectedArgumentWithTargetHint(stderr, flags.Arg(0), targetValues)
		return 2
	}
	if *showDiff && !*dryRun {
		fmt.Fprintln(stderr, "remove failed: --diff requires --dry-run")
		return 2
	}
	if err := validatePresentationFlags("remove", *jsonOutput, *verbose, *showDiff); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}

	targets := targetValues.strings()
	scope, err := singleScopeValue(scopeValues)
	if err != nil {
		fmt.Fprintf(stderr, "remove failed: %s\n", humanDiagnosticError(err))
		return 2
	}

	result, err := authoring.RemoveHook(ctx, authoringExecutionOptions(*manifestPath, *dryRun), authoring.RemoveHookRequest{
		ResourceName: resourceName,
		Targets:      targets,
		Scope:        scope,
	})
	if err != nil {
		printAuthoringOperationError(stderr, "remove", *manifestPath, err)
		return 1
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceHook, result)); err != nil {
				fmt.Fprintf(stderr, "remove failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("remove", clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceHook, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceHook, result)); err != nil {
			fmt.Fprintf(stderr, "remove failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("removed", clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceHook, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func splitRemoveHookArgs(args []string) (string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 1, hookAuthoringFlagTakesValue)
	if err != nil {
		return "", nil, err
	}
	var resourceName string
	if len(positionals) != 0 {
		resourceName = positionals[0]
	}
	if strings.TrimSpace(resourceName) == "" {
		return "", nil, fmt.Errorf("missing hook name")
	}
	name, err := authoring.CleanHookName(resourceName)
	if err != nil {
		return "", nil, err
	}
	return name, flagArgs, nil
}
