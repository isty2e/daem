package cli

import (
	"context"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func prepareAddSkillInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"add", "skill"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	sourcePath := flags.String("path", "", "skill path inside a git repository")
	ref := flags.String("ref", "", "git branch, tag, or full 40/64-hex commit selector")
	name := flags.String("name", "", "agent-visible skill name")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "target to add; may be repeated")
	flags.Var(&scopes, "scope", "scope for the skill resource")
	arguments := prepareAuthoringArguments(flags, args, 1)
	response := newCommandResponse("add skill", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		source, err := requiredAuthoringOperand(arguments, "missing skill source")
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

		result, err := authoring.AddSkill(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.AddSkillRequest{
			SourceArg: source, SourcePath: *sourcePath, Ref: *ref, Name: strings.TrimSpace(*name), Targets: targets.strings(), Scope: scope, Mode: authoring.DefaultLocalSourceMode(),
		})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("add", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkill, result, *manifest, *verbose, *diff)
	}}
}

func prepareAddSkillGroupInvocation(ctx context.Context, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{"add", "skill-group"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	var members skillGroupMemberFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	sourcePath := flags.String("path", "", "skill group root path inside a git repository")
	ref := flags.String("ref", "", "git branch, tag, or full 40/64-hex commit selector")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&members, "member", "skill member under the source root; may be repeated")
	flags.Var(&targets, "target", "target to add; may be repeated")
	flags.Var(&scopes, "scope", "scope for the skill_group resource")
	arguments := prepareAuthoringArguments(flags, args, 1)
	response := newCommandResponse("add skill-group", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		source, err := requiredAuthoringOperand(arguments, "missing skill_group source root")
		if err != nil {
			return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		if err := validateAuthoringPresentation("add", arguments.output, *verbose, *diff, *dryRun); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		names, err := skillGroupMembers(members)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("add failed: "))
		}
		scope, err := singleScopeValue(scopes)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("add failed: "))
		}

		result, err := authoring.AddSkillGroup(ctx, authoringExecutionOptions(*manifest, *dryRun), authoring.AddSkillGroupRequest{
			SourceArg: source, SourcePath: *sourcePath, Ref: *ref, Names: names, Targets: targets.strings(), Scope: scope, Mode: authoring.DefaultLocalSourceMode(),
		})
		if err != nil {
			return response.failure(operationFailure, authoringFailureDiagnostic("add", *manifest, err))
		}
		return renderAuthoringResult(response, clipresent.AuthoringOperationAdd, clipresent.AuthoringResourceSkillGroup, result, *manifest, *verbose, *diff)
	}}
}
