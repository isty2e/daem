package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func prepareAddHookInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"add", "hook"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	matcher := flags.String("matcher", "", "hook matcher")
	timeout := flags.String("timeout", "", "positive command timeout duration")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "target to add; may be repeated")
	flags.Var(&scopes, "scope", "scope for the hook resource")
	arguments := prepareAuthoringArguments(flags, args, 3)
	response := newCommandResponse("add hook", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		name, event, command, err := addHookOperands(arguments)
		if err != nil {
			return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		if err := validateAuthoringPresentation("add", arguments.output, *verbose, *diff, *dryRun); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		timeoutSeconds := 0
		if *timeout != "" {
			duration, err := parsePositiveDuration("--timeout", *timeout)
			if err != nil {
				return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("add failed: "))
			}
			if duration%time.Second != 0 {
				return response.failure(usageFailure, clipresent.NewFailureDiagnostic("add failed: --timeout must be representable as whole seconds"))
			}
			timeoutSeconds = int(duration / time.Second)
		}
		scope, err := singleScopeValue(scopes)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("add failed: "))
		}

		result, err := authoring.AddHook(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.AddHookRequest{
			Name: name, Event: event, Command: command, Matcher: strings.TrimSpace(*matcher), TimeoutSeconds: timeoutSeconds, Targets: targets.strings(), Scope: scope,
		})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("add", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceHook, result, *manifest, *verbose, *diff)
	}}
}

func addHookOperands(plan authoringArgumentPlan) (string, string, string, error) {
	positionals, err := plan.operands()
	if err != nil {
		return "", "", "", err
	}
	if len(positionals) == 0 || strings.TrimSpace(positionals[0]) == "" {
		return "", "", "", fmt.Errorf("missing hook name")
	}
	if len(positionals) < 2 || strings.TrimSpace(positionals[1]) == "" {
		return "", "", "", fmt.Errorf("missing hook event")
	}
	if len(positionals) < 3 || strings.TrimSpace(positionals[2]) == "" {
		return "", "", "", fmt.Errorf("missing hook command")
	}
	name, err := authoring.CleanHookName(positionals[0])
	if err != nil {
		return "", "", "", err
	}
	return name, strings.TrimSpace(positionals[1]), strings.TrimSpace(positionals[2]), nil
}
