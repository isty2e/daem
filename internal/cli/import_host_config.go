package cli

import (
	"flag"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

func prepareImportHostConfigInvocation(args []string, stdout, stderr io.Writer, options commandOptions) *preparedInvocation {
	path := []string{"import", "host-config"}
	flags := newCommandFlagSet(path, stderr)
	manifest := flags.String("manifest", "", "selected manifest")
	update := flags.Bool("update-source", false, "update this declaration's local source membership")
	var targets targetFlagValues
	var scopes scopeFlagValues
	flags.Var(&targets, "target", "one supported initial target")
	flags.Var(&scopes, "scope", "initial project or global scope")
	hostPath := flags.String("host-path", "", "initial host-root-relative document path")
	ownership := flags.String("ownership", "keys", "initial keys or explicit document ownership")
	syntax := flags.String("syntax", "", "initial grammar override")
	sourceDir := flags.String("source-dir", "", "initial local source directory")
	export := flags.String("source", "", "new local source destination for an update; must not exist")
	merge := flags.Bool("merge", false, "add initial capture to an existing manifest")
	dryRun := flags.Bool("dry-run", false, "preview without writes or recovery")
	showDiff := flags.Bool("diff", false, "show only selected values with --dry-run")
	yes := flags.Bool("yes", false, "approve the named-file disclosure bound by --expect-plan")
	expect := flags.String("expect-plan", "", "fingerprint from the reviewed dry-run")
	verbose := flags.Bool("verbose", false, "include additional failure evidence")
	flags.Bool("json", false, "emit masked structured output")
	var keys replacementKeyValues
	flags.Var(&keys, "key", "RFC 6901 key selector; repeatable")
	arguments := prepareAuthoringArguments(flags, args, 1)
	response := newCommandResponse("import host-config", arguments.output, stdout, stderr)
	return &preparedInvocation{response: response, execute: func() int {
		fail := func(code commandFailureClass, err error) int {
			return response.failure(code, humanFailureDiagnostic(err))
		}
		if commandHelpRequested(args) {
			printCommandUsage(path, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic(path, err))
		}
		name, err := requiredAuthoringOperand(arguments, "import host-config requires one declaration id")
		if err != nil {
			return fail(usageFailure, err)
		}
		if *dryRun && *yes || *expect != "" && !*yes || *yes && *expect == "" {
			return fail(usageFailure, fmt.Errorf("use --dry-run to review, then --yes --expect-plan <fingerprint> to approve the named-file edit"))
		}
		if err := validateAuthoringPresentation("import host-config", arguments.output, *verbose, *showDiff, *dryRun); err != nil {
			return fail(usageFailure, err)
		}
		if !*dryRun && !*yes && (arguments.output == requestedJSON || !options.confirmation.allowsInteractiveAuthorization()) {
			return fail(usageFailure, fmt.Errorf("capture requires terminal confirmation or --yes --expect-plan <fingerprint>"))
		}
		seen := make(map[string]bool)
		flags.Visit(func(flag *flag.Flag) { seen[flag.Name] = true })
		if *update {
			for _, flag := range []string{"target", "scope", "host-path", "ownership", "syntax", "source-dir", "merge"} {
				if seen[flag] {
					return fail(usageFailure, fmt.Errorf("--%s is initial-only; --update-source uses the selected declaration", flag))
				}
			}
		} else if seen["source"] {
			return fail(usageFailure, fmt.Errorf("--source requires --update-source; initial capture uses --source-dir"))
		}
		var command workflowadopt.HostConfigCaptureCommand
		if *update {
			command, err = workflowadopt.PlanHostConfigUpdateCapture(options.context, workflowadopt.HostConfigUpdateCommandInput{ManifestPath: *manifest, Name: name, SourcePath: *export, Keys: keys})
		} else {
			if len(targets) != 1 || len(scopes) != 1 {
				return fail(usageFailure, fmt.Errorf("initial capture requires exactly one --target and one --scope"))
			}
			command, err = workflowadopt.PlanHostConfigInitialCapture(options.context, workflowadopt.HostConfigInitialCommandInput{ManifestPath: *manifest, Name: name, Target: string(targets[0]), Scope: string(scopes[0]), HostPath: *hostPath, Ownership: *ownership, Syntax: *syntax, SourceDir: *sourceDir, Keys: keys, Merge: *merge})
		}
		if err != nil {
			if workflowadopt.IsHostConfigCaptureInputError(err) {
				return fail(usageFailure, err)
			}
			return fail(operationFailure, err)
		}
		if recovery := command.Recovery(); recovery != nil {
			return executeCaptureRecoveryInvocation(recovery, *dryRun, *yes, *expect, arguments.output, stdout, options, response)
		}
		plan := command.Capture()
		defer plan.Close()
		mode := "dry-run"
		if !*dryRun {
			mode = "captured"
		}
		disclosure, err := clipresent.HostConfigCaptureDisclosure(options.context, plan, mode)
		if err != nil {
			return fail(operationFailure, err)
		}
		encoded, err := disclosure.JSON()
		if err != nil {
			return fail(operationFailure, err)
		}
		if arguments.output != requestedJSON {
			preview := disclosure
			preview.Mode = "preview"
			if err := clipresent.PrintHostConfigCapture(stdout, preview, *showDiff); err != nil {
				return fail(operationFailure, err)
			}
		}
		if !*dryRun {
			if *yes {
				if *expect != plan.Fingerprint() {
					return fail(operationFailure, fmt.Errorf("capture plan differs from the approved fingerprint; review a new dry-run"))
				}
			} else {
				confirmed, err := options.confirmation.prompt("import host-config")
				if err != nil {
					return fail(operationFailure, err)
				}
				if !confirmed {
					return fail(operationFailure, fmt.Errorf("capture canceled"))
				}
			}
			result, err := workflowadopt.ExecuteHostConfigCapture(options.context, plan, plan.ApprovalPath())
			if err != nil {
				if result.Committed {
					err = fmt.Errorf("capture committed source/declaration changes but cleanup failed: %w", err)
				}
				return fail(operationFailure, err)
			}
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return fail(operationFailure, err)
		}
		if arguments.output == requestedJSON {
			if _, err := fmt.Fprintln(stdout, string(encoded)); err != nil {
				markOutputFailureReported(stdout)
				return 1
			}
		}
		if arguments.output != requestedJSON && !*dryRun {
			message := "captured: source/declaration saved; host files unchanged"
			if plan.RetiresDeclaration() {
				message = "retired: declaration removed; old source and host files unchanged"
			}
			if _, err := fmt.Fprintln(stdout, message); err != nil {
				markOutputFailureReported(stdout)
				return 1
			}
		}
		return 0
	}}
}
