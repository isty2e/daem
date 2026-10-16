package cli

import (
	"context"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func prepareRemoveInstructionInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"remove", "instruction"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "target to remove; may be repeated")
	flags.Var(&scopes, "scope", "scope to remove from")
	arguments := prepareAuthoringArguments(flags, args, 1)
	response := newCommandResponse("remove instruction", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		name, err := requiredAuthoringOperand(arguments, "missing instruction name")
		if err == nil {
			name, err = authoring.CleanInstructionName(name)
		}
		if err != nil {
			return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		if err := validateAuthoringPresentation("remove", arguments.output, *verbose, *diff, *dryRun); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		scope, err := singleScopeValue(scopes)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("remove failed: "))
		}

		result, err := authoring.RemoveInstruction(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.RemoveInstructionRequest{ResourceName: name, Targets: targets.strings(), Scope: scope})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("remove", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceInstructions, result, *manifest, *verbose, *diff)
	}}
}
