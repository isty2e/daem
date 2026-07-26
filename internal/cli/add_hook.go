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

func runAddHook(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"add", "hook"}, stdout, 0)
		return 0
	}

	name, event, command, flagArgs, err := splitAddHookArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"add", "hook"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"add", "hook"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	matcher := flags.String("matcher", "", "hook matcher")
	timeoutValue := flags.String("timeout", "", "positive command timeout duration")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "target to add; may be repeated")
	flags.Var(&scopeValues, "scope", "scope for the hook resource")

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
	timeoutSeconds := 0
	if *timeoutValue != "" {
		duration, err := parsePositiveDuration("--timeout", *timeoutValue)
		if err != nil {
			fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
			return 2
		}
		if duration%time.Second != 0 {
			fmt.Fprintln(stderr, "add failed: --timeout must be representable as whole seconds")
			return 2
		}
		timeoutSeconds = int(duration / time.Second)
	}

	targets := targetValues.strings()
	scope, err := addScope(scopeValues)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		return 2
	}
	result, err := authoring.AddHook(ctx, authoringExecutionOptions(*manifestPath, *dryRun), authoring.AddHookRequest{
		Name:           name,
		Event:          event,
		Command:        command,
		Matcher:        strings.TrimSpace(*matcher),
		TimeoutSeconds: timeoutSeconds,
		Targets:        targets,
		Scope:          scope,
	})
	if err != nil {
		printAuthoringOperationError(stderr, "add", *manifestPath, err)
		return 1
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceHook, result)); err != nil {
				fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("add", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceHook, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceHook, result)); err != nil {
			fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("added", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceHook, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func splitAddHookArgs(args []string) (string, string, string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 3, hookAuthoringFlagTakesValue)
	if err != nil {
		return "", "", "", nil, err
	}
	if len(positionals) == 0 || strings.TrimSpace(positionals[0]) == "" {
		return "", "", "", nil, fmt.Errorf("missing hook name")
	}
	if len(positionals) < 2 || strings.TrimSpace(positionals[1]) == "" {
		return "", "", "", nil, fmt.Errorf("missing hook event")
	}
	if len(positionals) < 3 || strings.TrimSpace(positionals[2]) == "" {
		return "", "", "", nil, fmt.Errorf("missing hook command")
	}
	cleanName, err := authoring.CleanHookName(positionals[0])
	if err != nil {
		return "", "", "", nil, err
	}
	return cleanName, strings.TrimSpace(positionals[1]), strings.TrimSpace(positionals[2]), flagArgs, nil
}

func hookAuthoringFlagTakesValue(name string) bool {
	switch name {
	case "manifest", "matcher", "timeout", "target", "scope":
		return true
	default:
		return false
	}
}
