package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func runAddSkill(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"add", "skill"}, stdout, 0)
		return 0
	}

	sourceArg, flagArgs, err := splitAddSkillArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"add", "skill"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"add", "skill"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	sourcePath := flags.String("path", "", "skill path inside a git repository")
	ref := flags.String("ref", "", "git branch, tag, or full 40/64-hex commit selector")
	name := flags.String("name", "", "agent-visible skill name")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "target to add; may be repeated")
	flags.Var(&scopeValues, "scope", "scope for the skill resource")

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
	scope, err := addScope(scopeValues)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		return 2
	}

	request := authoring.AddSkillRequest{
		SourceArg:  sourceArg,
		SourcePath: *sourcePath,
		Ref:        *ref,
		Name:       strings.TrimSpace(*name),
		Targets:    targets,
		Scope:      scope,
		Mode:       authoring.DefaultLocalSourceMode(),
	}

	result, err := authoring.AddSkill(ctx, authoringExecutionOptions(*manifestPath, *dryRun), request)
	if err != nil {
		printAuthoringOperationError(stderr, "add", *manifestPath, err)
		return 1
	}

	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkill, result)); err != nil {
				fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("add", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkill, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkill, result)); err != nil {
			fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("added", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkill, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func splitAddSkillArgs(args []string) (string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 1, addSkillFlagTakesValue)
	if err != nil {
		return "", nil, err
	}
	var source string
	if len(positionals) != 0 {
		source = positionals[0]
	}
	if strings.TrimSpace(source) == "" {
		return "", nil, fmt.Errorf("missing skill source")
	}
	return source, flagArgs, nil
}

func addSkillFlagTakesValue(name string) bool {
	switch name {
	case "manifest", "path", "ref", "name", "target", "scope":
		return true
	default:
		return false
	}
}

func runAddSkillGroup(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"add", "skill-group"}, stdout, 0)
		return 0
	}

	sourceArg, flagArgs, err := splitAddSkillGroupArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		printAuthoringArgumentCorrection(stderr, []string{"add", "skill-group"}, err, args)
		return 2
	}

	flags := newCommandFlagSet([]string{"add", "skill-group"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	var memberValues skillGroupMemberFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	sourcePath := flags.String("path", "", "skill group root path inside a git repository")
	ref := flags.String("ref", "", "git branch, tag, or full 40/64-hex commit selector")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	showDiff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&memberValues, "member", "skill member under the source root; may be repeated")
	flags.Var(&targetValues, "target", "target to add; may be repeated")
	flags.Var(&scopeValues, "scope", "scope for the skill_group resource")

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

	names, err := skillGroupMembers(memberValues)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		return 2
	}
	targets := targetValues.strings()
	scope, err := addScope(scopeValues)
	if err != nil {
		fmt.Fprintf(stderr, "add failed: %s\n", humanDiagnosticError(err))
		return 2
	}

	result, err := authoring.AddSkillGroup(ctx, authoringExecutionOptions(*manifestPath, *dryRun), authoring.AddSkillGroupRequest{
		SourceArg:  sourceArg,
		SourcePath: *sourcePath,
		Ref:        *ref,
		Names:      names,
		Targets:    targets,
		Scope:      scope,
		Mode:       authoring.DefaultLocalSourceMode(),
	})
	if err != nil {
		printAuthoringOperationError(stderr, "add", *manifestPath, err)
		return 1
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkillGroup, result)); err != nil {
				fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("add", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkillGroup, result), clipresent.HumanOptions{Verbose: *verbose})
		if *showDiff {
			clipresent.PrintManifestDiff(stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
		}
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintManifestAuthoringJSON(stdout, clipresent.ManifestAuthoringJSONFrom(clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkillGroup, result)); err != nil {
			fmt.Fprintf(stderr, "add failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}
	clipresent.PrintAuthoringChangeWithOptions(stdout, clipresent.AuthoringChangeFrom("added", clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkillGroup, result), clipresent.HumanOptions{Verbose: *verbose})
	return 0
}

func splitAddSkillGroupArgs(args []string) (string, []string, error) {
	positionals, flagArgs, err := splitAuthoringArgs(args, 1, addSkillGroupFlagTakesValue)
	if err != nil {
		return "", nil, err
	}
	var source string
	if len(positionals) != 0 {
		source = positionals[0]
	}
	if strings.TrimSpace(source) == "" {
		return "", nil, fmt.Errorf("missing skill_group source root")
	}
	return source, flagArgs, nil
}

func addSkillGroupFlagTakesValue(name string) bool {
	switch name {
	case "manifest", "path", "ref", "member", "target", "scope":
		return true
	default:
		return false
	}
}
