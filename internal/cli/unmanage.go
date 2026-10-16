package cli

import (
	"context"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func runUnmanage(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if handled, code := handleGroupHelp("unmanage", args, stdout, stderr); handled {
		return code
	}
	if args[0] == "extension" {
		return prepareUnmanageExtensionInvocation(ctx, args[1:], stdout, stderr).execute()
	}
	fmt.Fprintf(stderr, "unknown unmanage resource %q\n", args[0])
	fmt.Fprintln(stderr, "next: run daem help unmanage")
	return 2
}

func prepareUnmanageExtensionInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"unmanage", "extension"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview management release without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "optional exact extension target safety filter")
	flags.Var(&scopes, "scope", "optional exact extension scope safety filter")
	arguments := prepareAuthoringArguments(flags, args, 1)
	response := newCommandResponse("unmanage extension", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		id, err := extensionIDOperand(arguments)
		if err != nil {
			return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		if err := validateAuthoringPresentation("unmanage", arguments.output, *verbose, *diff, *dryRun); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		selectedTarget, err := unmanageTarget(targets)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("unmanage failed: "))
		}
		scope, err := singleScopeValue(scopes)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("unmanage failed: "))
		}
		mode := authoring.UnmanageModeWrite
		if *dryRun {
			mode = authoring.UnmanageModeDryRun
		}

		result, err := authoring.UnmanageExtension(ctx, authoring.UnmanageExtensionRequest{ManifestPath: *manifest, ID: id, Target: selectedTarget, Scope: target.Scope(scope), Mode: mode})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("unmanage", *manifest, err))
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		if arguments.output == requestedJSON {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.UnmanageExtensionJSONFrom(result)); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("unmanage failed: write json: "))
			}
			return 0
		}
		clipresent.PrintUnmanageExtensionWithOptions(stdout, result, clipresent.HumanOptions{Verbose: *verbose})
		if *diff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}}
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
