package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func runOutdated(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"outdated"}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"outdated"}, stderr)

	manifestPath := flags.String("manifest", "", "path to daem.toml")
	check := flags.Bool("check", false, "exit non-zero when lockable sources would change")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if err := validatePresentationFlags("outdated", *jsonOutput, *verbose, false); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}

	progress := newLockProgressRenderer(*jsonOutput, stderr, options)
	result, err := workflowlock.RunOutdated(options.context, workflowlock.OutdatedInput{
		ManifestPath: *manifestPath,
		LockfilePath: "",
		SourceEvents: progress.SourceSink(),
		LockEvents:   progress.LockSink(),
	})
	progress.Close()
	if err != nil {
		fmt.Fprintf(stderr, "outdated failed: %s\n", humanDiagnosticError(err))
		printLockWorkflowHints(stderr, *manifestPath, err)
		return 1
	}

	mode := "outdated"
	if *check {
		mode = "check"
	}
	if *jsonOutput {
		if err := clipresent.PrintJSON(stdout, clipresent.JSONInput{
			Command:       "outdated",
			Mode:          mode,
			ManifestPath:  result.ManifestPath,
			LockfilePath:  result.LockfilePath,
			PreviousFound: result.PreviousFound,
			Lockfile:      result.Lockfile,
			Delta:         result.Delta,
		}); err != nil {
			fmt.Fprintf(stderr, "outdated failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
		if *check && result.Delta.HasChanges() {
			return 1
		}
		return 0
	}

	if result.Delta.HasChanges() {
		fmt.Fprintf(stdout, "outdated: lockfile can be refreshed: %s\n", humanDiagnosticText(result.LockfilePath))
		clipresent.PrintDeltaSummaryWithOptions(stdout, result.Delta, clipresent.HumanOptions{Verbose: *verbose})
		printLockCommandHint(stdout, result.ManifestPath)
		if *check {
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "outdated: lockfile is current: %s (checked=%d)\n", humanDiagnosticText(result.LockfilePath), result.Delta.Counts().Unchanged)
	if *verbose {
		clipresent.PrintDeltaSummaryWithOptions(stdout, result.Delta, clipresent.HumanOptions{Verbose: true})
	}
	return 0
}
