package clipresent

import (
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func printNativeRecoverPlan(output io.Writer, plan journal.NativeRecoveryPlan, options HumanOptions) {
	fmt.Fprintf(output, "recover: %s\noperation: %s\n", plan.Classification(), plan.OperationID())
	if options.Verbose {
		fmt.Fprintf(output, "operation directory: %s\n", Escape(plan.OperationDir()))
	}
	for _, action := range plan.DocumentActions() {
		fmt.Fprintf(output, "%s target=%s scope=%s destination=%q (selected document areas only)\n", action.Kind, action.Address.Target(), action.Address.Scope(), action.Address.AggregateRoot().String())
	}
	if plan.Classification() == recovery.ClassificationNeedsFinalize {
		fmt.Fprintln(output, "finalize_claims (document claims and bindings; host data is unchanged)")
	}
	if plan.Blocked() {
		fmt.Fprintln(output, "recovery blocked: selected document differs from admitted recovery states; preserve recovery evidence")
	}
}

func nativeRecoveryJSONPayload(mode, phase, fileSetFence string, plan journal.NativeRecoveryPlan) recoveryPlanJSONOutput {
	actions := make([]recoveryPlanJSONAction, 0)
	for _, action := range plan.DocumentActions() {
		actions = append(actions, recoveryPlanJSONAction{Kind: string(action.Kind), Target: string(action.Address.Target()), Scope: string(action.Address.Scope()), Destination: action.Address.AggregateRoot().String(), Detail: "selected document areas only; unrelated values are retained"})
	}
	if plan.Classification() == recovery.ClassificationNeedsFinalize {
		actions = append(actions, recoveryPlanJSONAction{Kind: string(recovery.ActionKindFinalizeClaims), Detail: "finalize document claims and bindings without replaying host writes"})
	}
	return recoveryPlanJSONOutput{
		SchemaVersion: contractversion.RecoveryJSON, Command: "recover", Mode: mode, Phase: phase, AuthorityKind: journal.RecoveryAuthorityActiveJournal,
		OperationID: plan.OperationID(), OperationDir: plan.OperationDir(), Classification: string(plan.Classification()), ActionCount: len(actions), HasErrors: plan.HasErrors(),
		Actions: actions, CleanupObligations: []recoveryCleanupObligationOutput{}, ContinuingFileSetFence: fileSetFence,
	}
}
