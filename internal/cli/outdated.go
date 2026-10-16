package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func prepareOutdatedInvocation(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"outdated"}, stderr)

	manifestPath := flags.String("manifest", "", "path to daem.toml")
	check := flags.Bool("check", false, "exit non-zero when lockable sources would change")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	plan := prepareGoArguments(flags, args)
	response := newCommandResponse("outdated", plan.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"outdated"}, stdout, 0)
			return 0
		}
		if err := plan.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"outdated"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()))
		}
		if err := validatePresentationFlags("outdated", plan.output == requestedJSON, *verbose, false); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}

		progress := newLockProgressRenderer(plan.output == requestedJSON, stderr, options)
		result, err := workflowlock.RunOutdated(options.context, workflowlock.OutdatedInput{
			ManifestPath: *manifestPath,
			LockfilePath: "",
			SourceEvents: progress.SourceSink(),
			LockEvents:   progress.LockSink(),
		})
		progress.Close()
		if err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("outdated failed: ").WithGuidance(lockWorkflowGuidance(*manifestPath, err)...))
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}

		mode := "outdated"
		if *check {
			mode = "check"
		}
		if plan.output == requestedJSON {
			if err := clipresent.PrintJSON(stdout, clipresent.JSONInput{
				Command:       "outdated",
				Mode:          mode,
				ManifestPath:  result.ManifestPath,
				LockfilePath:  result.LockfilePath,
				PreviousFound: result.PreviousFound,
				Lockfile:      result.Lockfile,
				Delta:         result.Delta,
			}); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("outdated failed: write json: "))
			}
			if *check && result.Delta.HasChanges() {
				return 1
			}
			return 0
		}

		if result.Delta.HasChanges() {
			fmt.Fprintf(stdout, "outdated: lockfile can be refreshed: %s\n", humanDiagnosticText(result.LockfilePath))
			clipresent.PrintDeltaSummaryWithOptions(stdout, result.Delta, clipresent.HumanOptions{Verbose: *verbose})
			printLockCommandHint(stdout, result.ManifestPath, *manifestPath)
			if *check {
				return 1
			}
			return 0
		}

		fmt.Fprintf(stdout, "outdated: lockfile is current: %s (%s)\n", humanDiagnosticText(result.LockfilePath), clipresent.CountLabel(result.Delta.Counts().Unchanged, "lock entry", "lock entries"))
		if *verbose {
			clipresent.PrintDeltaSummaryWithOptions(stdout, result.Delta, clipresent.HumanOptions{Verbose: true})
		}
		return 0
	}}
}
