package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	diagnoseworkflow "github.com/isty2e/daem/internal/workflow/diagnose"
	probeworkflow "github.com/isty2e/daem/internal/workflow/probe"
)

func runDoctor(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	options commandOptions,
) int {
	if commandHelpRequested(args) {
		printCommandUsage([]string{"doctor"}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"doctor"}, stderr)

	var targetValues targetFlagValues
	manifestPath := flags.String("manifest", "", "optional path to daem.toml")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	allTargets := flags.Bool("all-targets", false, "diagnose every supported target")
	flags.Var(&targetValues, "target", "target to diagnose; may be repeated")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 0 {
		printUnexpectedArgumentWithTargetHint(stderr, flags.Arg(0), targetValues)
		return 2
	}
	if err := validatePresentationFlags("doctor", *jsonOutput, *verbose, false); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}

	manifestExplicit := false
	targetExplicit := false
	flags.Visit(func(flag *flag.Flag) {
		switch flag.Name {
		case "manifest":
			manifestExplicit = true
		case "target":
			targetExplicit = true
		}
	})
	if *allTargets && targetExplicit {
		fmt.Fprintln(stderr, "doctor failed: --all-targets cannot be combined with --target")
		return 2
	}
	assessment, err := options.assessPlatform()
	if err != nil {
		fmt.Fprintf(stderr, "doctor failed: observe platform runtime: %s\n", humanDiagnosticError(err))
		return 1
	}

	result, err := diagnoseworkflow.Run(options.context, diagnoseworkflow.Input{
		ManifestPath:     *manifestPath,
		ManifestExplicit: manifestExplicit,
		TargetExplicit:   targetExplicit,
		AllTargets:       *allTargets,
		TargetValues:     targetValues.strings(),
	}, assessment)
	if err != nil {
		fmt.Fprintf(stderr, "doctor failed: %s\n", humanDiagnosticError(err))
		if errors.Is(err, diagnoseworkflow.ErrInvalidTargetSelection) {
			return 2
		}
		return 1
	}

	if *jsonOutput {
		if err := clipresent.PrintDoctorJSON(stdout, clipresent.DoctorJSONInput{
			ManifestPath:     result.ManifestPath,
			ManifestExplicit: result.ManifestExplicit,
			Selection:        result.Selection,
			Checks:           result.Checks,
		}); err != nil {
			fmt.Fprintf(stderr, "doctor failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
	} else {
		targets := result.Selection.Targets()
		targetNames := make([]string, 0, len(targets))
		for _, selectedTarget := range targets {
			targetNames = append(targetNames, string(selectedTarget))
		}
		fmt.Fprintf(stdout, "targets: %s\n", strings.Join(targetNames, ","))
		clipresent.PrintDoctorChecksWithOptions(stdout, result.Checks, clipresent.HumanOptions{Verbose: *verbose})
	}
	if result.HasErrors {
		return 1
	}

	return 0
}

func runProbe(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) int {
	if handled, exitCode := handleGroupHelp("probe", args, stdout, stderr); handled {
		return exitCode
	}
	if args[0] != "mcp-server" {
		fmt.Fprintf(stderr, "probe failed: unknown probe subject %q\n", args[0])
		fmt.Fprintln(stderr, "next: run daem help probe")
		return 2
	}
	if commandHelpRequested(args[1:]) {
		printCommandUsage([]string{"probe", "mcp-server"}, stdout, 0)
		return 0
	}

	flags := newCommandFlagSet([]string{"probe", "mcp-server"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "disclose probe effects without launching the server")
	yes := flags.Bool("yes", false, "launch the selected locked MCP server and run initialize")
	jsonOutput := flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	timeout := flags.Duration("timeout", probeworkflow.DefaultTimeout, "probe timeout, such as 30s")
	flags.Var(&targetValues, "target", "target selector; repeat syntax is shared, but probe admits one distinct target")
	flags.Var(&scopeValues, "scope", "scope selector; repeat syntax is shared, but probe admits one distinct scope")

	if len(args) < 2 {
		fmt.Fprintln(stderr, "probe failed: mcp-server name is required")
		return 2
	}
	serverName := args[1]
	if err := flags.Parse(args[2:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "probe failed: unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if *dryRun && *yes {
		fmt.Fprintln(stderr, "probe failed: --dry-run and --yes are mutually exclusive")
		return 2
	}
	if err := validatePresentationFlags("probe", *jsonOutput, *verbose, false); err != nil {
		fmt.Fprintln(stderr, humanDiagnosticError(err))
		return 2
	}
	if *jsonOutput && !*dryRun && !*yes {
		fmt.Fprintln(stderr, "probe failed: --json requires --dry-run or --yes")
		return 2
	}
	if *timeout <= 0 {
		fmt.Fprintln(stderr, "probe failed: --timeout must be positive")
		return 2
	}
	targets := targetValues.strings()
	if len(targets) > 1 {
		fmt.Fprintln(stderr, "probe failed: probe accepts at most one distinct --target")
		return 2
	}
	scope, err := singleScopeValue(scopeValues)
	if err != nil {
		fmt.Fprintf(stderr, "probe failed: %s\n", humanDiagnosticError(err))
		return 2
	}
	targetValue := ""
	if len(targets) == 1 {
		targetValue = targets[0]
	}
	interactiveConfirmation := !*dryRun && !*yes
	if interactiveConfirmation && !options.confirmation.allowsInteractiveAuthorization() {
		printInteractiveConfirmationRequired(stderr, "probe", "probe")
		fmt.Fprintln(stderr, "next: run daem probe mcp-server <name> --dry-run first, then rerun with --yes when the effects are acceptable")
		return 2
	}

	mode := probeworkflow.ModeExecute
	if *dryRun || interactiveConfirmation {
		mode = probeworkflow.ModeDryRun
	}
	prepared, err := probeworkflow.Prepare(options.context, probeworkflow.CommandInput{
		ServerName:   serverName,
		ManifestPath: *manifestPath,
		LockfilePath: "",
		TargetValue:  targetValue,
		ScopeValue:   scope,
		Mode:         mode,
		Timeout:      *timeout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "probe failed: %s\n", humanDiagnosticError(err))
		printMissingManifestInitHint(stderr, *manifestPath, err)
		return 1
	}
	defer prepared.Close()
	result := prepared.Disclosure()
	if mode == probeworkflow.ModeExecute {
		result, err = prepared.Execute(options.context, options.probeExecutor)
		if err != nil {
			fmt.Fprintf(stderr, "probe failed: %s\n", humanDiagnosticError(err))
			return 1
		}
	}

	report := clipresent.MCPProbeReportFrom(result)
	if *jsonOutput {
		if err := clipresent.PrintMCPProbeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "probe failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
	} else {
		clipresent.PrintMCPProbeReportWithOptions(stdout, report, clipresent.MCPProbeHumanOptions{
			Verbose:              *verbose,
			AwaitingConfirmation: interactiveConfirmation,
		})
	}
	if result.HasRuntimeErrors() {
		return 1
	}
	if interactiveConfirmation {
		confirmed, err := options.confirmation.prompt("probe")
		if err != nil {
			printConfirmationFailure(stderr, "probe", err)
			return 1
		}
		if !confirmed {
			fmt.Fprintln(stderr, "probe canceled")
			return 1
		}
		result, err = prepared.Execute(options.context, options.probeExecutor)
		if err != nil {
			fmt.Fprintf(stderr, "probe failed: %s\n", humanDiagnosticError(err))
			return 1
		}
		clipresent.PrintMCPProbeReportWithOptions(
			stdout,
			clipresent.MCPProbeReportFrom(result),
			clipresent.MCPProbeHumanOptions{Verbose: *verbose},
		)
		if result.HasRuntimeErrors() {
			return 1
		}
	}
	return 0
}
