package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func runRemove(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if handled, code := handleGroupHelp("remove", args, stdout, stderr); handled {
		return code
	}
	if prepared := prepareRemoveInvocation(ctx, args[0], args[1:], stdout, stderr); prepared != nil {
		return prepared.execute()
	}
	fmt.Fprintf(stderr, "unknown remove resource %q\n", args[0])
	fmt.Fprintln(stderr, "next: run daem help remove")
	return 2
}

func prepareRemoveInvocation(ctx context.Context, subject string, args []string, stdout, stderr io.Writer) *preparedInvocation {
	switch subject {
	case "extension":
		return prepareRemoveExtensionInvocation(ctx, args, stdout, stderr)
	case "instruction":
		return prepareRemoveInstructionInvocation(ctx, args, stdout, stderr)
	case "hook":
		return prepareRemoveHookInvocation(ctx, args, stdout, stderr)
	case "mcp-server":
		return prepareRemoveMCPInvocation(ctx, args, stdout, stderr)
	case "skill":
		return prepareRemoveSkillInvocation(ctx, args, stdout, stderr)
	default:
		return nil
	}
}

func prepareRemoveExtensionInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"remove", "extension"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "optional extension target filter; claude-code, codex, opencode, pi, or antigravity-cli")
	flags.Var(&scopes, "scope", "optional extension scope filter; project or global")
	arguments := prepareAuthoringArguments(flags, args, 1)
	response := newCommandResponse("remove extension", arguments.output, stdout, stderr)

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
		if err := validateAuthoringPresentation("remove", arguments.output, *verbose, *diff, *dryRun); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		scope, err := singleScopeValue(scopes)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("remove failed: "))
		}

		result, err := authoring.RemoveExtension(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.RemoveExtensionRequest{ID: id, Targets: targets.strings(), Scope: scope})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("remove", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceExtension, result, *manifest, *verbose, *diff)
	}}
}

func prepareRemoveMCPInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"remove", "mcp-server"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "target selector; accepts claude-code, opencode, codex, or antigravity-cli")
	flags.Var(&scopes, "scope", "scope selector; claude-code uses project, codex/opencode use project or explicit global, antigravity-cli requires global")
	arguments := prepareAuthoringArguments(flags, args, 1)
	response := newCommandResponse("remove mcp-server", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		name, err := requiredAuthoringOperand(arguments, "missing mcp-server name")
		if err == nil {
			name, err = authoring.CleanMCPServerName(name)
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

		result, err := authoring.RemoveMCPServer(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.RemoveMCPServerRequest{Name: name, Targets: targets.strings(), Scope: scope})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("remove", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceMCPServer, result, *manifest, *verbose, *diff)
	}}
}

func prepareRemoveSkillInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"remove", "skill"}
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
	response := newCommandResponse("remove skill", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		key, err := requiredAuthoringOperand(arguments, "missing skill resource key")
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

		result, err := authoring.RemoveSkill(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.RemoveSkillRequest{ResourceKey: strings.TrimSpace(key), Targets: targets.strings(), Scope: scope})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("remove", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceSkill, result, *manifest, *verbose, *diff)
	}}
}
