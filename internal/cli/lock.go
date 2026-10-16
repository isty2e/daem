package cli

import (
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func prepareLockInvocation(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"lock"}, stderr)
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "resolve and validate without writing the lockfile")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	plan := prepareGoArguments(flags, args)
	response := newCommandResponse("lock", plan.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"lock"}, stdout, 0)
			return 0
		}
		if err := plan.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"lock"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()))
		}
		if err := validatePresentationFlags("lock", plan.output == requestedJSON, *verbose, false); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}

		progress := newLockProgressRenderer(plan.output == requestedJSON, stderr, options)
		result, err := workflowlock.RunLock(options.context, workflowlock.LockInput{
			ManifestPath: *manifestPath,
			LockfilePath: "",
			DryRun:       *dryRun,
			SourceEvents: progress.SourceSink(),
			LockEvents:   progress.LockSink(),
		})
		progress.Close()
		if err != nil {
			diagnostic := humanFailureDiagnostic(err).WithPrefix("lock failed: ").WithGuidance(lockWorkflowGuidance(*manifestPath, err)...)
			return response.failure(operationFailure, diagnostic)
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}

		if *dryRun {
			if plan.output == requestedJSON {
				if err := clipresent.PrintJSON(stdout, clipresent.JSONInput{
					Command:       "lock",
					Mode:          "dry-run",
					ManifestPath:  result.ManifestPath,
					LockfilePath:  result.LockfilePath,
					PreviousFound: result.PreviousFound,
					Lockfile:      result.Lockfile,
					Delta:         result.Delta,
				}); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("lock failed: write json: "))
				}
				return 0
			}
			clipresent.PrintDryRunSummaryWithOptions(stdout, clipresent.DryRunSummaryInput{
				LockfilePath: result.LockfilePath,
				Lockfile:     result.Lockfile,
				Delta:        result.Delta,
				NextCommand:  lockCommandHint(result.ManifestPath, *manifestPath),
			}, clipresent.HumanOptions{Verbose: *verbose})
			return 0
		}

		if plan.output == requestedJSON {
			if err := clipresent.PrintJSON(stdout, clipresent.JSONInput{
				Command:       "lock",
				Mode:          "write",
				ManifestPath:  result.ManifestPath,
				LockfilePath:  result.LockfilePath,
				PreviousFound: result.PreviousFound,
				Lockfile:      result.Lockfile,
				Delta:         result.Delta,
			}); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("lock failed: write json: "))
			}
			return 0
		}

		fmt.Fprintf(stdout, "wrote lockfile: %s\n", humanDiagnosticText(result.LockfilePath))
		clipresent.PrintDeltaSummaryWithOptions(stdout, result.Delta, clipresent.HumanOptions{Verbose: *verbose})
		return 0
	}}
}

func lockWorkflowGuidance(manifestPath string, err error) []clipresent.Guidance {
	var guidance []clipresent.Guidance
	if initialize, present := missingManifestInitGuidance(manifestPath, err); present {
		guidance = append(guidance, initialize)
	}
	var commandError workflowlock.CommandError
	if errors.As(err, &commandError) {
		if source, present := clipresent.MissingSourceGuidance(err); present {
			guidance = append(
				guidance,
				source,
				manifestCommandGuidance([]string{"lock"}, commandError.ManifestPath, manifestHintOptions(manifestPath, false), "--dry-run"),
			)
		}
	}
	return guidance
}
