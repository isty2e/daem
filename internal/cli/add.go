package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func runAdd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if handled, code := handleGroupHelp("add", args, stdout, stderr); handled {
		return code
	}
	if prepared := prepareAddInvocation(ctx, args[0], args[1:], stdout, stderr); prepared != nil {
		return prepared.execute()
	}
	fmt.Fprintf(stderr, "unknown add resource %q\n", args[0])
	fmt.Fprintln(stderr, "next: run daem help add")
	return 2
}

func prepareAddInvocation(ctx context.Context, subject string, args []string, stdout, stderr io.Writer) *preparedInvocation {
	switch subject {
	case "extension":
		return prepareAddExtensionInvocation(ctx, args, stdout, stderr)
	case "instruction":
		return prepareAddInstructionInvocation(ctx, args, stdout, stderr)
	case "hook":
		return prepareAddHookInvocation(ctx, args, stdout, stderr)
	case "mcp-server":
		return prepareAddMCPInvocation(ctx, args, stdout, stderr)
	case "skill":
		return prepareAddSkillInvocation(ctx, args, stdout, stderr)
	case "skill-group":
		return prepareAddSkillGroupInvocation(ctx, args, stdout, stderr)
	default:
		return nil
	}
}

func prepareAddExtensionInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"add", "extension"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "extension target; claude-code, codex, opencode, pi, or antigravity-cli")
	flags.Var(&scopes, "scope", "scope for the selected extension carrier; project or explicit global")
	arguments := prepareAuthoringArguments(flags, args, 2)
	response := newCommandResponse("add extension", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		id, source, err := addExtensionOperands(arguments)
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

		result, err := authoring.AddExtension(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.AddExtensionRequest{ID: id, Source: source, Targets: targets.strings(), Scope: scope})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("add", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceExtension, result, *manifest, *verbose, *diff)
	}}
}

func addExtensionOperands(plan authoringArgumentPlan) (string, string, error) {
	positionals, err := plan.operands()
	if err != nil {
		return "", "", err
	}
	if len(positionals) == 0 {
		return "", "", fmt.Errorf("missing extension id")
	}
	if len(positionals) < 2 || strings.TrimSpace(positionals[1]) == "" {
		return "", "", fmt.Errorf("missing extension source")
	}
	id, err := authoring.CleanExtensionID(positionals[0])
	if err != nil {
		return "", "", err
	}
	return id, positionals[1], nil
}

type mcpArgFlagValues []string

func (values *mcpArgFlagValues) String() string         { return fmt.Sprint([]string(*values)) }
func (values *mcpArgFlagValues) Set(value string) error { *values = append(*values, value); return nil }

func prepareAddMCPInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"add", "mcp-server"}
	flags := newCommandFlagSet(path, stderr)
	var rawTargets mcpInputFlagValues
	var rawScopes mcpInputFlagValues
	var rawEnv mcpInputFlagValues
	var argv mcpArgFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&argv, "arg", "argv entry for the MCP server command; may be repeated")
	flags.Var(&rawEnv, "env-from", "CHILD=SOURCE environment variable names; SOURCE must use uppercase letters, digits, or underscore")
	flags.Var(&rawTargets, "target", "target to add")
	flags.Var(&rawScopes, "scope", "scope for the MCP server; project rows may omit it, but global MCP rows require explicit --scope global")
	arguments := prepareAuthoringArguments(flags, args, 2)
	response := newCommandResponse("add mcp-server", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, mcpInputBindingDiagnostic(path, arguments, err, args))
		}
		name, command, err := addMCPOperands(arguments)
		if err != nil {
			return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
		}
		if err := validateAuthoringPresentation("add", arguments.output, *verbose, *diff, *dryRun); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		targets, err := mcpInputTargets(rawTargets)
		if err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		scope, err := mcpInputScope(rawScopes)
		if err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		env, err := mcpInputEnvironment(rawEnv)
		if err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}

		result, err := authoring.AddMCPServer(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.AddMCPServerRequest{Name: name, Command: command, Args: []string(argv), Env: env, Targets: targets, Scope: scope})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("add", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceMCPServer, result, *manifest, *verbose, *diff)
	}}
}

func addMCPOperands(plan authoringArgumentPlan) (string, string, error) {
	positionals, err := plan.operands()
	if err != nil {
		return "", "", err
	}
	if len(positionals) == 0 || strings.TrimSpace(positionals[0]) == "" {
		return "", "", fmt.Errorf("missing mcp-server name")
	}
	if len(positionals) < 2 || strings.TrimSpace(positionals[1]) == "" {
		return "", "", fmt.Errorf("missing mcp-server command")
	}
	name, err := authoring.CleanMCPServerName(positionals[0])
	if err != nil {
		return "", "", err
	}
	return name, positionals[1], nil
}
