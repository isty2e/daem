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
	clipresent.PrintRelationActionsWithOptions(output, planning.Reconciliation.Relations(), options)
	clipresent.PrintRelationOrderActionsWithOptions(output, planning.Reconciliation.RelationOrders(), options)
	clipresent.PrintCarrierAdoptionActionsWithOptions(output, planning.Reconciliation.CarrierAdoptions(), options)
	clipresent.PrintCarrierAbsenceActionsWithOptions(output, planning.Reconciliation.CarrierAbsences(), options)
}

func applyWorkflowGuidance(manifestPath string, result applyworkflow.CommandResult, err error) []clipresent.Guidance {
	selectedManifest := result.ManifestPath
	if selectedManifest == "" {
		selectedManifest = manifestPath
	}
	return append(applyInputWorkflowGuidance(manifestPath, result, err), clipresent.NativeReplacementGuidance(result.Reconciliation, selectedManifest)...)
}
