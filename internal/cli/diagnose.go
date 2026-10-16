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

func prepareDoctorInvocation(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	options commandOptions,
) *preparedInvocation {
	flags := newCommandFlagSet([]string{"doctor"}, stderr)

	var targetValues targetFlagValues
	manifestPath := flags.String("manifest", "", "optional path to daem.toml")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	allTargets := flags.Bool("all-targets", false, "diagnose every supported target")
	flags.Var(&targetValues, "target", "target to diagnose; may be repeated")
	arguments := prepareGoArguments(flags, args)
	response := newCommandResponse("doctor", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"doctor"}, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"doctor"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, unexpectedArgumentDiagnostic(flags.Arg(0), targetValues))
		}
		if err := validatePresentationFlags("doctor", arguments.output == requestedJSON, *verbose, false); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
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
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("doctor failed: --all-targets cannot be combined with --target"))
		}
		assessment, err := options.assessPlatform()
		if err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("doctor failed: observe platform runtime: "))
		}

		result, err := diagnoseworkflow.Run(options.context, diagnoseworkflow.Input{
			ManifestPath:     *manifestPath,
			ManifestExplicit: manifestExplicit,
			TargetExplicit:   targetExplicit,
			AllTargets:       *allTargets,
			TargetValues:     targetValues.strings(),
		}, assessment)
		if err != nil {
			class := operationFailure
			if errors.Is(err, diagnoseworkflow.ErrInvalidTargetSelection) {
				class = usageFailure
			}
			return response.failure(class, humanFailureDiagnostic(err).WithPrefix("doctor failed: "))
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		if arguments.output == requestedJSON {
			if err := clipresent.PrintDoctorJSON(stdout, clipresent.DoctorJSONInput{
				ManifestPath:     result.ManifestPath,
				ManifestExplicit: result.ManifestExplicit,
				Selection:        result.Selection,
				Checks:           result.Checks,
			}); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("doctor failed: write json: "))
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
	}}
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
	return prepareProbeMCPInvocation(args[1:], stdout, stderr, options).execute()
}

func prepareProbeMCPInvocation(args []string, stdout io.Writer, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"probe", "mcp-server"}, stderr)

	var targetValues targetFlagValues
	var scopeValues scopeFlagValues
	manifestPath := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "disclose probe effects without launching the server")
	yes := flags.Bool("yes", false, "launch the selected locked MCP server and run initialize")
	flags.Bool("json", false, "emit structured JSON output")
	verbose := flags.Bool("verbose", false, "emit additional human-readable evidence")
	timeout := flags.Duration("timeout", probeworkflow.DefaultTimeout, "probe timeout, such as 30s")
	flags.Var(&targetValues, "target", "target selector; repeat syntax is shared, but probe admits one distinct target")
	flags.Var(&scopeValues, "scope", "scope selector; repeat syntax is shared, but probe admits one distinct scope")
	arguments := prepareNamedArguments(flags, args, "mcp-server name")
	response := newCommandResponse("probe mcp-server", arguments.options.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"probe", "mcp-server"}, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			if arguments.missing != nil {
				return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("probe failed: "))
			}
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"probe", "mcp-server"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()).WithPrefix("probe failed: "))
		}
		if *dryRun && *yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("probe failed: --dry-run and --yes are mutually exclusive"))
		}
		if err := validatePresentationFlags("probe", arguments.options.output == requestedJSON, *verbose, false); err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err))
		}
		if arguments.options.output == requestedJSON && !*dryRun && !*yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("probe failed: --json requires --dry-run or --yes"))
		}
		if *timeout <= 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("probe failed: --timeout must be positive"))
		}
		targets := targetValues.strings()
		if len(targets) > 1 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("probe failed: probe accepts at most one distinct --target"))
		}
		scope, err := singleScopeValue(scopeValues)
		if err != nil {
			return response.failure(usageFailure, humanFailureDiagnostic(err).WithPrefix("probe failed: "))
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
			ServerName:   arguments.name,
			ManifestPath: *manifestPath,
			LockfilePath: "",
			TargetValue:  targetValue,
			ScopeValue:   scope,
			Mode:         mode,
			Timeout:      *timeout,
		})
		if err != nil {
			diagnostic := humanFailureDiagnostic(err).WithPrefix("probe failed: ")
			if init, present := missingManifestInitGuidance(*manifestPath, err); present {
				diagnostic = diagnostic.WithGuidance(init)
			}
			return response.failure(operationFailure, diagnostic)
		}
		defer prepared.Close()
		result := prepared.Disclosure()
		if mode == probeworkflow.ModeExecute {
			result, err = prepared.Execute(options.context, options.probeExecutor)
			if err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("probe failed: "))
			}
		}

		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		report := clipresent.MCPProbeReportFrom(result)
		if arguments.options.output == requestedJSON {
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
	}}
}
