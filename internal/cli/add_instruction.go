package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func prepareAddInstructionInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"add", "instruction"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	sourcePath := flags.String("path", "", "instruction file path within the Git repository")
	ref := flags.String("ref", "", "Git branch, tag, or commit")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "target to add; may be repeated")
	flags.Var(&scopes, "scope", "scope for the instruction resource")
	arguments := prepareAuthoringArguments(flags, args, 2)
	response := newCommandResponse("add instruction", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		name, source, err := addInstructionOperands(arguments)
		if err != nil {
			return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		if err := validateAuthoringPresentation("add", arguments.output, *verbose, *diff, *dryRun); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		scope, err := singleScopeValue(scopes)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("add failed: "))
		}

		result, err := authoring.AddInstruction(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.AddInstructionRequest{
			Name: name, SourceArg: source, SourcePath: *sourcePath, Ref: *ref, Targets: targets.strings(), Scope: scope,
		})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("add", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceInstructions, result, *manifest, *verbose, *diff)
	}}
}

func addInstructionOperands(plan authoringArgumentPlan) (string, string, error) {
	positionals, err := plan.operands()
	if err != nil {
		return "", "", err
	}
	if len(positionals) == 0 || strings.TrimSpace(positionals[0]) == "" {
		return "", "", fmt.Errorf("missing instruction name")
	}
	if len(positionals) == 1 || strings.TrimSpace(positionals[1]) == "" {
		return "", "", fmt.Errorf("missing instruction source")
	}
	name, err := authoring.CleanInstructionName(positionals[0])
	if err != nil {
		return "", "", err
	}
	return name, strings.TrimSpace(positionals[1]), nil
}
