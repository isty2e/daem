package cli

import (
	"context"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func runUnmanage(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if handled, exitCode := handleGroupHelp("unmanage", args, stdout, stderr); handled {
		return exitCode
	}
	if args[0] == "extension" {
		return runUnmanageExtension(ctx, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown unmanage resource %q\n", args[0])
	fmt.Fprintln(stderr, "next: run daem help unmanage")
	return 2
}

func runUnmanageExtension(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"unmanage", "extension"}, stdout, 0)
		return 0
	}

	id, flagArgs, err := splitUnmanageExtensionArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "unmanage failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"unmanage", "extension"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"unmanage", "extension"}, stderr)
	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview management release without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "optional exact extension target safety filter")
	flags.Var(&scopeValues, "scope", "optional exact extension scope safety filter")

	if err := flags.Parse(flagArgs); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		printUnexpectedArgumentWithTargetHint(stderr, flags.Arg(0), targetValues)
		return 2
	}
	if *showDiff && !*dryRun {
		fmt.Fprintln(stderr, "unmanage failed: --diff requires --dry-run")
		return 2
	}
	if err := validatePresentationFlags("unmanage", *jsonOutput, *verbose, *showDiff); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}
	selectedTarget, err := unmanageTarget(targetValues)
	if err != nil {
		fmt.Fprintf(stderr, "unmanage failed: %s\n", humanDiagnosticError(err))
		return 2
	}
	selectedScope, err := addScope(scopeValues)
	if err != nil {
		fmt.Fprintf(stderr, "unmanage failed: %s\n", humanDiagnosticError(err))
		return 2
	}

	mode := authoring.UnmanageModeWrite
	if *dryRun {
		mode = authoring.UnmanageModeDryRun
	}
	result, err := authoring.UnmanageExtension(ctx, authoring.UnmanageExtensionRequest{
		ManifestPath: *manifestPath,
		ID:           id,
		Target:       selectedTarget,
		Scope:        target.Scope(selectedScope),
		Mode:         mode,
	})
	if err != nil {
		printAuthoringOperationError(stderr, "unmanage", *manifestPath, err)
		return 1
	}
	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(
			stdout,
			clipresent.UnmanageExtensionJSONFrom(result),
		); err != nil {
			fmt.Fprintf(stderr, "unmanage failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintUnmanageExtensionWithOptions(
		stdout,
		result,
		clipresent.HumanOptions{Verbose: *verbose},
	)
	if *showDiff {
		clipresent.PrintManifestDiff(
			stdout,
			result.ManifestPath,
			result.Original,
			result.ManifestPath,
			result.Content,
		)
	}
	return 0
}

func splitUnmanageExtensionArgs(args []string) (string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(
		args,
		1,
		extensionAuthoringFlagTakesValue,
	)
	if err != nil {
		return "", nil, err
	}
	var id string
	if len(positionals) != 0 {
		id = positionals[0]
	}
	cleanID, err := authoring.CleanExtensionID(id)
	if err != nil {
		return "", nil, err
	}
	return cleanID, flagArgs, nil
}

func unmanageTarget(values targetFlagValues) (target.Target, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > 1 {
		return "", fmt.Errorf("--target accepts at most one distinct target for this command")
	}
	return values[0], nil
}
