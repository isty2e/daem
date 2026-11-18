package cli

import (
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func printApplyPreviewPlan(output io.Writer, planning applyworkflow.CommandResult, mcpStatuses []clipresent.MCPStatus, options clipresent.HumanOptions) {
	clipresent.PrintPermissionProtection(output, planning.PermissionProtection)
	clipresent.PrintDryRunPlanWithOptions(output, clipresent.HumanPlanInput{
		Reconciliation: planning.Reconciliation,
		MCPStatuses:    mcpStatuses,
	}, options)
	clipresent.PrintLockOnlyResourceSummary(output, clipresent.LockOnlyResourcesFrom(planning.LockOnly))
	clipresent.PrintCarrierReconciliationWithOptions(output, planning.Reconciliation, options)
}

func applyWorkflowGuidance(manifestPath string, result applyworkflow.CommandResult, err error) []clipresent.Guidance {
	selectedManifest := result.ManifestPath
	if selectedManifest == "" {
		selectedManifest = manifestPath
	}
	return append(applyInputWorkflowGuidance(manifestPath, result, err),
		clipresent.AgentConfigReconciliationGuidance(result.Reconciliation, selectedManifest, result.AgentConfigDeclarations)...)
}
