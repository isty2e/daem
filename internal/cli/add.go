package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func runAdd(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if handled, exitCode := handleGroupHelp("add", args, stdout, stderr); handled {
		return exitCode
	}

	switch args[0] {
	case "extension":
		return runAddExtension(ctx, args[1:], stdout, stderr)
	case "instruction":
		return runAddInstruction(ctx, args[1:], stdout, stderr)
	case "hook":
		return runAddHook(ctx, args[1:], stdout, stderr)
	case "mcp-server":
		return runAddMCPServer(ctx, args[1:], stdout, stderr)
	case "skill":
		return runAddSkill(ctx, args[1:], stdout, stderr)
	case "skill-group":
		return runAddSkillGroup(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown add resource %q\n", args[0])
		fmt.Fprintln(stderr, "next: run daem help add")
		return 2
	}
}

func runAddExtension(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"add", "extension"}, stdout, 0)
		return 0
	}

	id, source, flagArgs, err := splitAddExtensionArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"add", "extension"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"add", "extension"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "extension target; claude-code, codex, opencode, pi, or antigravity-cli")
	flags.Var(&scopeValues, "scope", "scope for the selected extension carrier; project or explicit global")

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

	result, err := authoring.AddExtension(ctx, authoringExecutionOptions(*manifestPath, *dryRun), authoring.AddExtensionRequest{
		ID:      id,
		Source:  source,
		Targets: targets,
		Scope:   scope,
	})
	if err != nil {
		printAuthoringOperationError(stderr, "add", *manifestPath, err)
		return 1
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceExtension, result)); err != nil {
				fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("add", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceExtension, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceExtension, result)); err != nil {
			fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("added", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceExtension, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func splitAddExtensionArgs(args []string) (string, string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 2, extensionAuthoringFlagTakesValue)
	if err != nil {
		return "", "", nil, err
	}
	if len(positionals) == 0 {
		return "", "", nil, fmt.Errorf("missing extension id")
	}
	if len(positionals) < 2 || strings.TrimSpace(positionals[1]) == "" {
		return "", "", nil, fmt.Errorf("missing extension source")
	}
	cleanID, err := authoring.CleanExtensionID(positionals[0])
	if err != nil {
		return "", "", nil, err
	}
	return cleanID, positionals[1], flagArgs, nil
}

func extensionAuthoringFlagTakesValue(name string) bool {
	switch name {
	case "manifest", "target", "scope":
		return true
	default:
		return false
	}
}

type mcpArgFlagValues []string

func (values *mcpArgFlagValues) String() string {
	return fmt.Sprint([]string(*values))
}

func (values *mcpArgFlagValues) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runAddMCPServer(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"add", "mcp-server"}, stdout, 0)
		return 0
	}

	name, command, flagArgs, err := splitAddMCPServerArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"add", "mcp-server"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"add", "mcp-server"}, stderr)

	var argValues mcpArgFlagValues
	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&argValues, "arg", "argv entry for the MCP server command; may be repeated")
	flags.Var(&targetValues, "target", "target to add; accepts claude-code, opencode, codex, or antigravity-cli")
	flags.Var(&scopeValues, "scope", "scope for the MCP server; project rows may omit it, but global MCP rows require explicit --scope global")

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
	result, err := authoring.AddMCPServer(ctx, authoringExecutionOptions(*manifestPath, *dryRun), authoring.AddMCPServerRequest{
		Name:    name,
		Command: command,
		Args:    []string(argValues),
		Targets: targets,
		Scope:   scope,
	})
	if err != nil {
		printAuthoringOperationError(stderr, "add", *manifestPath, err)
		return 1
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceMCPServer, result)); err != nil {
				fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("add", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceMCPServer, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceMCPServer, result)); err != nil {
			fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("added", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceMCPServer, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func splitAddMCPServerArgs(args []string) (string, string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 2, addMCPServerFlagTakesValue)
	if err != nil {
		return "", "", nil, err
	}
	if len(positionals) == 0 || strings.TrimSpace(positionals[0]) == "" {
		return "", "", nil, fmt.Errorf("missing mcp-server name")
	}
	if len(positionals) < 2 || strings.TrimSpace(positionals[1]) == "" {
		return "", "", nil, fmt.Errorf("missing mcp-server command")
	}
	cleanName, err := authoring.CleanMCPServerName(positionals[0])
	if err != nil {
		return "", "", nil, err
	}
	return cleanName, positionals[1], flagArgs, nil
}

func addMCPServerFlagTakesValue(name string) bool {
	switch name {
	case "manifest", "arg", "target", "scope":
		return true
	default:
		return false
	}
}
