package journal

import (
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// NativeRecoveryDocumentAction is disclosure, not an executable legacy action.
// A representative alias identifies each single physical document group.
type NativeRecoveryDocumentAction struct {
	Address aggregate.DocumentAddress
	Kind    recovery.ActionKind
}

func (plan NativeRecoveryPlan) DocumentActions() []NativeRecoveryDocumentAction {
	aliases := plan.DocumentAliases()
	result := make([]NativeRecoveryDocumentAction, 0, len(aliases))
	for index, group := range aliases {
		if len(group) == 0 {
			continue
		}
		kind := recovery.ActionKindNoOp
		if plan.Blocked() {
			kind = recovery.ActionKindError
		} else if plan.assessment.disposition == nativeDocumentsRollback && plan.assessment.stages[index].restoration.Changed() {
			kind = recovery.ActionKindRestoreWrite
			if !plan.assessment.stages[index].restoration.Document().Exists() {
				kind = recovery.ActionKindRestoreDelete
			}
		}
		result = append(result, NativeRecoveryDocumentAction{Address: group[0], Kind: kind})
	}
	return result
}

type nativeSelectedPhaseError struct{ cause error }

func (failure *nativeSelectedPhaseError) Error() string {
	return "native selected document does not admit recovery"
}
func (failure *nativeSelectedPhaseError) Unwrap() error { return failure.cause }
