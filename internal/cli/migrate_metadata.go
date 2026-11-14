package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/migrate"
)

func prepareMigrateMetadataInvocation(args []string, stdout, stderr io.Writer, options commandOptions) *preparedInvocation {
	flags := newCommandFlagSet([]string{"migrate", "metadata"}, stderr)
	manifest := flags.String("manifest", "", "manifest selecting the shared metadata registry")
	dryRun := flags.Bool("dry-run", false, "preview the whole-registry conversion without writing")
	yes := flags.Bool("yes", false, "execute the disclosed conversion without prompting")
	flags.Bool("json", false, "emit structured JSON")
	recoverConversion := flags.Bool("recover", false, "recover interrupted metadata conversion or clean inactive metadata")
	var references []string
	flags.Func("reference", "independently known legacy lookup path; may be repeated", func(path string) error {
		if path == "" {
			return fmt.Errorf("reference path is required")
		}
		references = append(references, path)
		return nil
	})
	arguments := prepareGoArguments(flags, args)
	response := newCommandResponse("migrate metadata", arguments.output, stdout, stderr)

	return &preparedInvocation{response: response, execute: func() int {
		if commandHelpRequested(args) {
			printCommandUsage([]string{"migrate", "metadata"}, stdout, 0)
			return 0
		}
		if err := arguments.bind(flags); err != nil {
			return response.failure(usageFailure, flagBindingDiagnostic([]string{"migrate", "metadata"}, err))
		}
		if flags.NArg() != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: flags.Arg(0)}).Error()))
		}
		if *dryRun && *yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("migrate metadata failed: --dry-run and --yes are mutually exclusive"))
		}
		if *recoverConversion && len(references) != 0 {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("migrate metadata failed: --recover uses its recorded selection; --reference applies to a new conversion"))
		}
		if arguments.output == requestedJSON && !*dryRun && !*yes {
			return response.failure(usageFailure, clipresent.NewFailureDiagnostic("migrate metadata failed: --json requires --dry-run or --yes"))
		}
		interactive := !*dryRun && !*yes
		if interactive && !options.confirmation.allowsInteractiveAuthorization() {
			printInteractiveConfirmationRequired(stderr, "migrate metadata", "metadata conversion")
			return 2
		}
		planMetadata := migrate.PlanMetadata
		if options.metadataMigrationPlan != nil {
			planMetadata = options.metadataMigrationPlan
		}
		prepared, err := planMetadata(options.context, migrate.MetadataInput{
			ManifestPath: *manifest, Recover: *recoverConversion, References: references,
		})
		if err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("migrate metadata failed: "))
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
			clipresent.PrintMetadataMigration(stdout, mode, plan)
		}
		if *dryRun {
			if arguments.output == requestedJSON {
				if err := clipresent.PrintMetadataMigrationJSON(stdout, mode, plan, nil); err != nil {
					return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("migrate metadata failed: write json: "))
				}
			}
			return 0
		}
		if interactive && plan.Action != "already_converted" && plan.Action != "none" {
			confirmed, err := options.confirmation.prompt("migrate metadata")
			if err != nil {
				printConfirmationFailure(stderr, "migrate metadata", err)
				return 1
			}
			if !confirmed {
				fmt.Fprintln(stderr, "metadata conversion canceled")
				return 1
			}
		}
		result, executeErr := prepared.Execute(options.context)
		if arguments.output == requestedJSON {
			if executeErr != nil {
				result = plan
			}
			if err := clipresent.PrintMetadataMigrationJSON(stdout, mode, result, executeErr); err != nil {
				return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix("migrate metadata failed: write json: "))
			}
		}
		if executeErr != nil {
			if arguments.output != requestedJSON {
				fmt.Fprintf(stderr, "migrate metadata failed: %s\n", humanDiagnosticError(executeErr))
			}
			return 1
		}
		if arguments.output != requestedJSON {
			fmt.Fprintf(stdout, "metadata conversion: %s\n", result.Action)
		}
		return 0
	}}
}
