package cli

import (
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	listworkflow "github.com/isty2e/daem/internal/workflow/list"
)

func runList(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	if handled, exitCode := handleGroupHelp("list", args, stdout, stderr); handled {
		return exitCode
	}
	subcommand := args[0]
	if !isListSubject(subcommand) {
		fmt.Fprintf(stderr, "unknown list resource %q\n", subcommand)
		fmt.Fprintln(stderr, "next: run daem help list")
		return 2
	}
	return prepareListInvocation(subcommand, args[1:], stdout, stderr, options).execute()
}

func isListSubject(subject string) bool {
	return subject == "resources" || subject == "outputs" || subject == "paths"
}

func prepareListInvocation(subcommand string, args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	path := []string{"list", subcommand}
	flags := newCommandFlagSet(path, stderr)
	var targetValues targetFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	flags.Var(&targetValues, "target", "target to enumerate; may be repeated")
	plan := prepareGoArguments(flags, args)
	response := newCommandResponse("list "+subcommand, plan.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		if err := plan.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()))
		}
		if err := validatePresentationFlags("list", plan.output == requestedJSON, *verbose, false); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		targets := targetValues.strings()
		if err := options.context.Err(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("list failed: "))
		}
		if subcommand == "outputs" {
			result, err := listworkflow.RunOutputs(options.context, listworkflow.Input{
				ManifestPath: *manifestPath,
				TargetValues: targets,
			})
			if err != nil {
				return response.failure(operationFailure, listFailureDiagnostic(err, result.ManifestPath, *manifestPath))
			}
			if err := response.selectOrdinaryResult(); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err))
			}
			if plan.output == requestedJSON {
				if err := clipresent.PrintListOutputsJSON(stdout, result.Inventory); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("list failed: write json: "))
				}
				return 0
			}
			clipresent.PrintInventoryReportWithOptions(stdout, result.Inventory, clipresent.HumanOptions{Verbose: *verbose})
			return 0
		}
		if subcommand == "paths" {
			result, err := listworkflow.RunPaths(options.context, listworkflow.Input{
				ManifestPath: *manifestPath,
				TargetValues: targets,
			})
			if err != nil {
				return response.failure(operationFailure, listFailureDiagnostic(err, result.ManifestPath, *manifestPath))
			}
			if err := response.selectOrdinaryResult(); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err))
			}
			if plan.output == requestedJSON {
				if err := clipresent.PrintListPathsJSON(stdout, result.ManifestPath, result.Inventory); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("list failed: write json: "))
				}
				return 0
			}
			clipresent.PrintListPathsWithOptions(
				stdout,
				result.ManifestPath,
				result.Inventory,
				clipresent.HumanOptions{Verbose: *verbose},
			)
			return 0
		}

		result, err := listworkflow.Run(options.context, listworkflow.Input{
			ManifestPath: *manifestPath,
			TargetValues: targets,
		})
		if err != nil {
			return response.failure(operationFailure, listFailureDiagnostic(err, result.ManifestPath, *manifestPath))
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		rows := clipresent.ListRows(result.Environment, result.SkillGroups(), result.Selection)
		if plan.output == requestedJSON {
			if err := clipresent.PrintListResourcesJSON(stdout, result.ManifestPath, rows); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("list failed: write json: "))
			}
			return 0
		}
		clipresent.PrintListRowsWithOptions(stdout, result.ManifestPath, rows, clipresent.HumanOptions{Verbose: *verbose})
		return 0
	}}
}

func listFailureDiagnostic(err error, selectedManifestPath, requestedManifestPath string) clipresent.FailureDiagnostic {
	diagnostic := humanFailureDiagnostic(err).WithPrefix("list failed: ")
	manifestPath := selectedManifestPath
	if manifestPath == "" {
		manifestPath = requestedManifestPath
	}
	if guidance, present := missingManifestInitGuidance(manifestPath, err); present {
		diagnostic = diagnostic.WithGuidance(guidance)
	}
	if errors.Is(err, targetselection.ErrInvalid) {
		diagnostic = diagnostic.WithGuidance(targetSelectionGuidance(selectedManifestPath, requestedManifestPath)...)
	}
	return diagnostic
}
