package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func prepareHostConfigAuthoring(ctx context.Context, operation clipresent.AuthoringOperation, args []string, stdout, stderr io.Writer) *preparedInvocation {
	path := []string{string(operation), "host-config"}
	flags := newCommandFlagSet(path, stderr)
	var targets targetFlagValues
	var scopes scopeFlagValues
	manifest := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview manifest change without writing")
	diff := flags.Bool("diff", false, "show manifest diff with --dry-run")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targets, "target", "one host target")
	flags.Var(&scopes, "scope", "one scope: project or global")

	var add authoring.AddHostConfigRequest
	positionalCount := 1
	adding := operation == clipresent.AuthoringOperationAdd
	if adding {
		positionalCount = 2
		flags.StringVar(&add.HostPath, "host-path", "", "destination relative to the selected host configuration root")
		flags.StringVar(&add.Ownership, "ownership", "", "keys (default) or document")
		flags.StringVar(&add.Syntax, "syntax", "", "configuration grammar; omit to use the host/path default")
		flags.StringVar(&add.SourcePath, "path", "", "file path within a Git source repository")
		flags.StringVar(&add.Ref, "ref", "", "Git source branch, tag, or full object ID")
		flags.StringVar(&add.VersionID, "version-id", "", "S3 object version ID")
		flags.StringVar(&add.Region, "region", "", "S3 region override")
	}
	arguments := prepareAuthoringArguments(flags, args, positionalCount)
	response := newCommandResponse(strings.Join(path, " "), arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		operands, err := arguments.operands()
		if err == nil && (len(operands) == 0 || strings.TrimSpace(operands[0]) == "") {
			err = fmt.Errorf("host configuration id is required")
		}
		if err == nil && adding && (len(operands) < 2 || strings.TrimSpace(operands[1]) == "") {
			err = fmt.Errorf("host configuration source is required")
		}
		if err != nil {
			return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		if err := validateAuthoringPresentation(string(operation), arguments.output, *verbose, *diff, *dryRun); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		selected, scope, err := hostConfigAuthoringPlacement(targets, scopes, adding)
		if err != nil {
			return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
		}

		options := authoringExecutionOptions(*manifest, *dryRun)
		var result authoring.OperationResult
		if adding {
			if err := validateHostConfigAddFlags(flags, add.HostPath); err != nil {
				return response.failure(usageFailure, authoringArgumentDiagnostic(path, err, args))
			}
			add.ID, add.SourceArg, add.Target, add.Scope = operands[0], operands[1], selected, scope
			result, err = authoring.AddHostConfig(ctx, options, add)
		} else {
			result, err = authoring.RemoveHostConfig(ctx, options, authoring.RemoveHostConfigRequest{ID: operands[0], Target: selected, Scope: scope})
		}
		if err != nil {
			diagnostic := authoringFailureDiagnostic(string(operation), *manifest, err)
			if errors.Is(err, authoring.ErrMissingGitRef) {
				diagnostic = diagnostic.WithGuidance(clipresent.InstructionGuidance("prefix a relative local path with ./ if it looks like an SSH locator"))
			}
			return response.failure(operationFailure, diagnostic)
		}
		return renderAuthoringResult(response, operation, clipresent.AuthoringResourceHostConfig, result, *manifest, *verbose, *diff)
	}}
}

func hostConfigAuthoringPlacement(targets targetFlagValues, scopes scopeFlagValues, adding bool) (target.Target, target.Scope, error) {
	if len(targets) > 1 || len(scopes) > 1 {
		return "", "", fmt.Errorf("host-config accepts at most one distinct --target and --scope")
	}
	if adding && (len(targets) == 0 || len(scopes) == 0) {
		return "", "", fmt.Errorf("add host-config requires an explicit --target and --scope")
	}
	var selected target.Target
	var scope target.Scope
	if len(targets) != 0 {
		selected = targets[0]
	}
	if len(scopes) != 0 {
		scope = scopes[0]
	}
	return selected, scope, nil
}

func validateHostConfigAddFlags(flags *flag.FlagSet, hostPath string) error {
	if strings.TrimSpace(hostPath) == "" {
		return fmt.Errorf("--host-path is required; --path selects a file inside a Git source repository")
	}
	var err error
	flags.Visit(func(value *flag.Flag) {
		switch value.Name {
		case "path", "ref", "version-id", "region", "ownership", "syntax":
			if err == nil && strings.TrimSpace(value.Value.String()) == "" {
				err = fmt.Errorf("--%s requires a nonempty value", value.Name)
			}
		}
	})
	return err
}
