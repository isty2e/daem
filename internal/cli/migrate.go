package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/migrate"
)

func runMigrate(args []string, stdout, stderr io.Writer, options commandOptions) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printCommandUsage([]string{"migrate"}, stdout, 0)
		return 0
	}
	if args[0] != "state" {
		fmt.Fprintf(stderr, "unknown migration subject %q\n", args[0])
		return 2
	}
	return prepareMigrateStateInvocation(args[1:], stdout, stderr, options).execute()
}

func prepareMigrateStateInvocation(args []string, stdout, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"migrate", "state"}, stderr)
	manifest := flags.String("manifest", "", "path to the default user manifest")
	dryRun := flags.Bool("dry-run", false, "preview metadata authority changes without writing")
	yes := flags.Bool("yes", false, "execute the disclosed migration without prompting")
	flags.Bool("json", false, "emit structured JSON")
	recoverMigration := flags.Bool("recover", false, "restore or finalize an interrupted state migration")
	arguments := prepareGoArguments(flags, args)
	response := newCommandResponse("migrate state", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"migrate", "state"}, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"migrate", "state"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()))
		}
		if *dryRun && *yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("migrate state failed: --dry-run and --yes are mutually exclusive"))
		}
		if arguments.output == requestedJSON && !*dryRun && !*yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("migrate state failed: --json requires --dry-run or --yes"))
		}
		interactive := !*dryRun && !*yes
		if interactive && !options.confirmation.allowsInteractiveAuthorization() {
			printInteractiveConfirmationRequired(stderr, "migrate state", "state migration")
			return 2
		}
		planState := migrate.PlanState
		if options.stateMigrationPlan != nil {
			planState = options.stateMigrationPlan
		}
		prepared, err := planState(options.context, migrate.StateInput{ManifestPath: *manifest, Recover: *recoverMigration})
		if err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("migrate state failed: "))
		}
		defer prepared.Close()
		if err := response.selectOrdinaryResult(); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err))
		}
		plan := prepared.Disclosure()
		mode := "write"
		if *dryRun {
			mode = "dry-run"
		}
		if arguments.output != requestedJSON {
			clipresent.PrintStateMigration(stdout, mode, plan)
		}
		if *dryRun {
			if arguments.output == requestedJSON {
				if err := clipresent.PrintStateMigrationJSON(stdout, mode, plan, nil); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("migrate state failed: write json: "))
				}
			}
			return 0
		}
		if interactive && plan.Action != "already_migrated" {
			confirmed, err := options.confirmation.prompt("migrate state")
			if err != nil {
				printConfirmationFailure(stderr, "migrate state", err)
				return 1
			}
			if !confirmed {
				fmt.Fprintln(stderr, "state migration canceled")
				return 1
			}
		}
		result, executeErr := prepared.Execute(options.context)
		if arguments.output == requestedJSON {
			if executeErr != nil {
				result = plan
			}
			if err := clipresent.PrintStateMigrationJSON(stdout, mode, result, executeErr); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("migrate state failed: write json: "))
			}
		}
		if executeErr != nil {
			if arguments.output != requestedJSON {
				fmt.Fprintf(stderr, "migrate state failed: %s\n", humanDiagnosticError(executeErr))
			}
			return 1
		}
		if arguments.output != requestedJSON {
			fmt.Fprintf(stdout, "state migration: %s\n", result.Action)
		}
		return 0
	}}
}
