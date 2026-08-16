package cli

import (
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func runLock(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"lock"}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"lock"}, stderr)

	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "resolve and validate without writing the lockfile")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if err := validatePresentationFlags("lock", *jsonOutput, *verbose, false); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}

	progress := newLockProgressRenderer(*jsonOutput, stderr, options)
	result, err := workflowlock.RunLock(options.context, workflowlock.LockInput{
		ManifestPath: *manifestPath,
		LockfilePath: "",
		DryRun:       *dryRun,
		SourceEvents: progress.SourceSink(),
		LockEvents:   progress.LockSink(),
	})
	progress.Close()
	if err != nil {
		fmt.Fprintf(stderr, "lock failed: %s\n", humanDiagnosticError(err))
		printLockWorkflowHints(stderr, *manifestPath, err)
		return 1
	}

	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintJSON(stdout, clipresent.JSONInput{
				Command:       "lock",
				Mode:          "dry-run",
				ManifestPath:  result.ManifestPath,
				LockfilePath:  result.LockfilePath,
				PreviousFound: result.PreviousFound,
				Lockfile:      result.Lockfile,
				Delta:         result.Delta,
			}); err != nil {
				fmt.Fprintf(stderr, "lock failed: write json: %s\n", humanDiagnosticError(err))
				return 1
			}
			return 0
		}
		clipresent.PrintDryRunSummaryWithOptions(stdout, clipresent.DryRunSummaryInput{
			LockfilePath: result.LockfilePath,
			Lockfile:     result.Lockfile,
			Delta:        result.Delta,
			NextCommand:  lockCommandHint(result.ManifestPath),
		}, clipresent.HumanOptions{Verbose: *verbose})
		return 0
	}

	if *jsonOutput {
		if err := clipresent.PrintJSON(stdout, clipresent.JSONInput{
			Command:       "lock",
			Mode:          "write",
			ManifestPath:  result.ManifestPath,
			LockfilePath:  result.LockfilePath,
			PreviousFound: result.PreviousFound,
			Lockfile:      result.Lockfile,
			Delta:         result.Delta,
		}); err != nil {
			fmt.Fprintf(stderr, "lock failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "wrote lockfile: %s\n", humanDiagnosticText(result.LockfilePath))
	clipresent.PrintDeltaSummaryWithOptions(stdout, result.Delta, clipresent.HumanOptions{Verbose: *verbose})

	return 0
}

func printLockWorkflowHints(output io.Writer, manifestPath string, err error) {
	printMissingManifestInitHint(output, manifestPath, err)

	var commandError workflowlock.CommandError
	if errors.As(err, &commandError) {
		printLockMissingSourceHint(output, commandError.ManifestPath, err)
	}
}
