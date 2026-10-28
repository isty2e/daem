package cli

import (
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

type replacementKeyValues []string

func (values *replacementKeyValues) String() string {
	return strings.Join(*values, ",")
}

func (values *replacementKeyValues) Set(raw string) error {
	*values = append(*values, raw)
	return nil
}

func prepareReplaceHostConfigInvocation(args []string, stdout, stderr io.Writer, options commandOptions) *preparedInvocation {
	path := []string{"replace", "host-config"}
	flags := newCommandFlagSet(path, stderr)
	manifest := flags.String("manifest", "", "path to daem.toml")
	dryRun := flags.Bool("dry-run", false, "preview selected replacement without effects")
	yes := flags.Bool("yes", false, "execute the fingerprint-approved replacement")
	expect := flags.String("expect-plan", "", "exact fingerprint from the reviewed dry-run")
	showDiff := flags.Bool("diff", false, "show selected current and declared values with --dry-run")
	verbose := flags.Bool("verbose", false, "include additional failure evidence")
	flags.Bool("json", false, "emit structured JSON output")
	var keys replacementKeyValues
	flags.Var(&keys, "key", "RFC 6901 selector narrowing the declaration's atomic keys; repeatable")
	arguments := prepareAuthoringArguments(flags, args, 1)
	response := newCommandResponse("replace host-config", arguments.output, stdout, stderr)
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
		name, operandErr := requiredAuthoringOperand(arguments, "replace host-config requires one declaration id")
		if operandErr != nil {
			return fail(usageFailure, fmt.Errorf("replace host-config requires one declaration id"))
		}
		if *dryRun && *yes {
			return fail(usageFailure, fmt.Errorf("--dry-run and --yes are mutually exclusive"))
		}
		if *showDiff && !*dryRun {
			return fail(usageFailure, fmt.Errorf("--diff requires --dry-run"))
		}
		if *expect != "" && !*yes {
			return fail(usageFailure, fmt.Errorf("--expect-plan requires --yes"))
		}
		if *yes && *expect == "" {
			return fail(usageFailure, fmt.Errorf("replacement requires --yes --expect-plan <fingerprint>; review --dry-run first"))
		}
		if arguments.output == requestedJSON && !*dryRun && !*yes {
			return fail(usageFailure, fmt.Errorf("--json requires --dry-run or --yes --expect-plan"))
		}
		if !*dryRun && !*yes && !options.confirmation.allowsInteractiveAuthorization() {
			return fail(usageFailure, fmt.Errorf("replacement needs terminal confirmation or --yes --expect-plan <fingerprint>; review --dry-run first"))
		}
		if err := validatePresentationFlags("replace host-config", arguments.output == requestedJSON, *verbose, *showDiff); err != nil {
			return fail(usageFailure, err)
		}
		plan, err := applyworkflow.PlanNativeReplacement(options.context, applyworkflow.NativeReplacementInput{ManifestPath: *manifest, ID: name, Keys: keys})
		if err != nil {
			if _, invalid := applyworkflow.InputErrorCause(err); invalid {
				return fail(usageFailure, err)
			}
			return fail(operationFailure, err)
		}
		defer plan.Close()
		disclosure := plan.Disclosure()
		mode := "dry-run"
		if !*dryRun {
			mode = "executed"
		}
		preparedJSON, err := clipresent.NativeReplacementJSON(disclosure, mode, len(disclosure.Result.Reconciliation.MutatingDecisions()))
		if err != nil {
			return fail(operationFailure, err)
		}
		if arguments.output != requestedJSON {
			if err := clipresent.PrintNativeReplacement(stdout, disclosure); err != nil {
				return fail(operationFailure, err)
			}
		}
		if !*dryRun {
			approved := *expect
			if !*yes {
				confirmed, err := options.confirmation.prompt("replace host-config")
				if err != nil {
					return fail(operationFailure, err)
				}
				if !confirmed {
					return 0
				}
				approved = disclosure.Fingerprint
			}
			if _, err := applyworkflow.ExecuteNativeReplacement(options.context, plan, approved, options.applyExecuteOptions); err != nil {
				return fail(operationFailure, err)
			}
		}
		if *showDiff {
			if err := clipresent.PrintNativeReplacementDiff(stdout, disclosure); err != nil {
				return fail(operationFailure, err)
			}
		}
		if err := response.selectOrdinaryResult(); err != nil {
			return fail(operationFailure, err)
		}
		if arguments.output == requestedJSON {
			if _, err := fmt.Fprintln(stdout, string(preparedJSON)); err != nil {
				markOutputFailureReported(stdout)
				return 1
			}
		} else if !*dryRun {
			fmt.Fprintln(stdout, "Selected host configuration keys are current.")
		}
		return 0
	}}
}
