package cli

import (
	"context"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func prepareUnmanageHostConfigInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"unmanage", "host-config"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview complete management release without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "optional exact host configuration target safety filter")
	flags.Var(&scopes, "scope", "optional exact host configuration scope safety filter")
	arguments := prepareAuthoringArguments(flags, args, 1)
	response := newCommandResponse("unmanage host-config", arguments.output, stdout, stderr)
	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		id, err := requiredAuthoringOperand(arguments, "host configuration id is required")
		if err != nil {
			return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		if err := validateAuthoringPresentation("unmanage", arguments.output, *verbose, *diff, *dryRun); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		selected, err := unmanageTarget(targets)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		scope, err := singleScopeValue(scopes)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		mode := authoring.UnmanageModeWrite
		if *dryRun {
			mode = authoring.UnmanageModeDryRun
		}
		result, err := authoring.UnmanageHostConfig(ctx, authoring.UnmanageHostConfigRequest{ManifestPath: *manifest, ID: id, Target: selected, Scope: target.Scope(scope), Mode: mode})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("unmanage", *manifest, err))
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		if arguments.output == requestedJSON {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.UnmanageHostConfigJSONFrom(result)); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err))
			}
			return 0
		}
		clipresent.PrintUnmanageHostConfigWithOptions(stdout, result, clipresent.HumanOptions{Verbose: *verbose})
		if *diff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}}
}
