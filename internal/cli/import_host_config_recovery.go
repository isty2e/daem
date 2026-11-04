package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

func executeCaptureRecoveryInvocation(plan *workflowadopt.HostConfigCaptureRecovery, dryRun, yes bool, expected string, output requestedOutput, stdout io.Writer, options commandOptions, response *commandResponse) int {
	fail := func(err error) int { return response.failure(operationFailure, humanFailureDiagnostic(err)) }
	mode := "recovered"
	if dryRun {
		mode = "dry-run"
	}
	disclosure := clipresent.HostConfigCaptureRecoveryDisclosure(plan, mode)
	encoded, err := disclosure.JSON()
	if err != nil {
		return fail(err)
	}
	if output != requestedJSON {
		if err := clipresent.PrintHostConfigCaptureRecovery(stdout, disclosure); err != nil {
			return fail(err)
		}
	}
	if !dryRun {
		if yes {
			if expected != plan.Fingerprint() {
				return fail(fmt.Errorf("capture recovery differs from the approved fingerprint; review a new dry-run"))
			}
		} else {
			confirmed, err := options.confirmation.prompt("recover import host-config")
			if err != nil {
				return fail(err)
			}
			if !confirmed {
				return fail(fmt.Errorf("capture recovery canceled"))
			}
		}
		result, err := workflowadopt.ExecuteHostConfigCaptureRecovery(options.context, plan, plan.Fingerprint())
		if err != nil {
			if result != "" {
				err = fmt.Errorf("capture recovery completed (%s) but cleanup failed; capture was not executed: %w", result, err)
			}
			return fail(err)
		}
	}
	if err := response.selectOrdinaryResult(); err != nil {
		return fail(err)
	}
	if output == requestedJSON {
		if _, err := fmt.Fprintln(stdout, string(encoded)); err != nil {
			markOutputFailureReported(stdout)
			return 1
		}
	} else if !dryRun {
		if _, err := fmt.Fprintln(stdout, "recovered: capture was not executed; review a separate dry-run"); err != nil {
			markOutputFailureReported(stdout)
			return 1
		}
	}
	return 0
}
