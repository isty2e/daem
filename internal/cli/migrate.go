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
	args = args[1:]
	if commandHelpRequested(args) {
		printCommandUsage([]string{"migrate", "state"}, stdout, 0)
		return 0
	}
	flags := newCommandFlagSet([]string{"migrate", "state"}, stderr)
	manifest := flags.String("manifest", "", "path to the default user manifest")
	dryRun := flags.Bool("dry-run", false, "preview metadata authority changes without writing")
	yes := flags.Bool("yes", false, "execute the disclosed migration without prompting")
	jsonOutput := flags.Bool("json", false, "emit structured JSON")
	recoverMigration := flags.Bool("recover", false, "restore or finalize an interrupted state migration")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if *dryRun && *yes {
		fmt.Fprintln(stderr, "migrate state failed: --dry-run and --yes are mutually exclusive")
		return 2
	}
	if *jsonOutput && !*dryRun && !*yes {
		fmt.Fprintln(stderr, "migrate state failed: --json requires --dry-run or --yes")
		return 2
	}
	interactive := !*dryRun && !*yes
	if interactive && !options.confirmation.allowsInteractiveAuthorization() {
		printInteractiveConfirmationRequired(stderr, "migrate state", "state migration")
		return 2
	}
	prepared, err := migrate.PlanState(options.context, migrate.StateInput{ManifestPath: *manifest, Recover: *recoverMigration})
	if err != nil {
		fmt.Fprintf(stderr, "migrate state failed: %s\n", humanDiagnosticError(err))
		return 1
	}
	defer prepared.Close()
	plan := prepared.Disclosure()
	mode := "write"
	if *dryRun {
		mode = "dry-run"
	}
	if !*jsonOutput {
		clipresent.PrintStateMigration(stdout, mode, plan)
	}
	if *dryRun {
		if *jsonOutput {
			if err := clipresent.PrintStateMigrationJSON(stdout, mode, plan, nil); err != nil {
				fmt.Fprintf(stderr, "migrate state failed: write json: %s\n", humanDiagnosticError(err))
				return 1
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
	if *jsonOutput {
		if executeErr != nil {
			result = plan
		}
		if err := clipresent.PrintStateMigrationJSON(stdout, mode, result, executeErr); err != nil {
			fmt.Fprintf(stderr, "migrate state failed: write json: %s\n", humanDiagnosticError(err))
			return 1
		}
	}
	if executeErr != nil {
		fmt.Fprintf(stderr, "migrate state failed: %s\n", humanDiagnosticError(executeErr))
		return 1
	}
	if !*jsonOutput {
		fmt.Fprintf(stdout, "state migration: %s\n", result.Action)
	}
	return 0
}
