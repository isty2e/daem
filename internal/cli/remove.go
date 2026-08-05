package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func runRemove(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if handled, exitCode := handleGroupHelp("remove", args, stdout, stderr); handled {
		return exitCode
	}

	switch args[0] {
	case "extension":
		return runRemoveExtension(ctx, args[1:], stdout, stderr)
	case "instruction":
		return runRemoveInstruction(ctx, args[1:], stdout, stderr)
	case "hook":
		return runRemoveHook(ctx, args[1:], stdout, stderr)
	case "mcp-server":
		return runRemoveMCPServer(ctx, args[1:], stdout, stderr)
	case "skill":
		return runRemoveSkill(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown remove resource %q\n", args[0])
		fmt.Fprintln(stderr, "next: run daem help remove")
		return 2
	}
}

func runRemoveExtension(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"remove", "extension"}, stdout, 0)
		return 0
	}

	id, flagArgs, err := splitRemoveExtensionArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "remove failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"remove", "extension"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"remove", "extension"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "optional extension target filter; claude-code, codex, opencode, pi, or antigravity-cli")
	flags.Var(&scopeValues, "scope", "optional extension scope filter; project or global")

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

	result, err := authoring.RemoveExtension(ctx, authoringExecutionOptions(*manifestPath, *dryRun), authoring.RemoveExtensionRequest{
		ID:      id,
		Targets: targets,
		Scope:   scope,
	})
	if err != nil {
		printAuthoringOperationError(stderr, "remove", *manifestPath, err)
		return 1
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceExtension, result)); err != nil {
				fmt.Fprintf(stderr, "remove failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("remove", clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceExtension, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceExtension, result)); err != nil {
			fmt.Fprintf(stderr, "remove failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("removed", clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceExtension, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func splitRemoveExtensionArgs(args []string) (string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 1, extensionAuthoringFlagTakesValue)
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

func runRemoveMCPServer(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"remove", "mcp-server"}, stdout, 0)
		return 0
	}

	name, flagArgs, err := splitRemoveMCPServerArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "remove failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"remove", "mcp-server"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"remove", "mcp-server"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "target selector; accepts claude-code, opencode, codex, or antigravity-cli")
	flags.Var(&scopeValues, "scope", "scope selector; claude-code uses project, codex/opencode use project or explicit global, antigravity-cli requires global")

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

	result, err := authoring.RemoveMCPServer(ctx, authoringExecutionOptions(*manifestPath, *dryRun), authoring.RemoveMCPServerRequest{
		Name:    name,
		Targets: targets,
		Scope:   scope,
	})
	if err != nil {
		printAuthoringOperationError(stderr, "remove", *manifestPath, err)
		printRemoveMCPServerNotFoundHint(stderr, err)
		return 1
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceMCPServer, result)); err != nil {
				fmt.Fprintf(stderr, "remove failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("remove", clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceMCPServer, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceMCPServer, result)); err != nil {
			fmt.Fprintf(stderr, "remove failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("removed", clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceMCPServer, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func runRemoveSkill(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"remove", "skill"}, stdout, 0)
		return 0
	}

	resourceKey, flagArgs, err := splitRemoveSkillArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "remove failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"remove", "skill"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"remove", "skill"}, stderr)

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

	request := authoring.RemoveSkillRequest{
		ResourceKey: resourceKey,
		Targets:     targets,
		Scope:       scope,
	}
	result, err := authoring.RemoveSkill(ctx, authoringExecutionOptions(*manifestPath, *dryRun), request)
	if err != nil {
		printAuthoringOperationError(stderr, "remove", *manifestPath, err)
		var splitErr authoring.SkillGroupPartialTargetRemovalError
		if errors.As(err, &splitErr) {
			printSkillGroupPartialTargetRemovalHint(stderr, splitErr.ResourceID, splitErr.RemainingTargets)
		}
		return 1
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceSkill, result)); err != nil {
				fmt.Fprintf(stderr, "remove failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("remove", clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceSkill, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceSkill, result)); err != nil {
			fmt.Fprintf(stderr, "remove failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("removed", clipresent.AuthoringOperationRemove, clipresent.AuthoringResourceSkill, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func splitRemoveMCPServerArgs(args []string) (string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 1, removeMCPServerFlagTakesValue)
	if err != nil {
		return "", nil, err
	}
	var name string
	if len(positionals) != 0 {
		name = positionals[0]
	}
	if strings.TrimSpace(name) == "" {
		return "", nil, fmt.Errorf("missing mcp-server name")
	}
	cleanName, err := authoring.CleanMCPServerName(name)
	if err != nil {
		return "", nil, err
	}
	return cleanName, flagArgs, nil
}

func removeMCPServerFlagTakesValue(name string) bool {
	switch name {
	case "manifest", "target", "scope":
		return true
	default:
		return false
	}
}

func printRemoveMCPServerNotFoundHint(output io.Writer, err error) {
	if err == nil || !strings.Contains(err.Error(), "mcp_server resource") || !strings.Contains(err.Error(), "not found") {
		return
	}
	fmt.Fprintln(output, "next: inspect declared [[mcp_server]] entries in the selected manifest, or add the server before removing it")
}

func splitRemoveSkillArgs(args []string) (string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 1, removeSkillFlagTakesValue)
	if err != nil {
		return "", nil, err
	}
	var resourceKey string
	if len(positionals) != 0 {
		resourceKey = positionals[0]
	}
	if strings.TrimSpace(resourceKey) == "" {
		return "", nil, fmt.Errorf("missing skill resource key")
	}
	return strings.TrimSpace(resourceKey), flagArgs, nil
}

func removeSkillFlagTakesValue(name string) bool {
	switch name {
	case "manifest", "target", "scope":
		return true
	default:
		return false
	}
}
