package recoveryoperation

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
	aliases, err := plan.DocumentAliases()
	if err != nil {
		return nil
	}
	result := make([]NativeRecoveryDocumentAction, 0, len(aliases))
	for index, group := range aliases {
		if len(group) == 0 {
			continue
		}
		kind := recovery.ActionKindNoOp
		if plan.Blocked() {
			kind = recovery.ActionKindError
		} else if plan.assessment.Classification() == recovery.ClassificationNeedsRollback {
			document, present := plan.assessment.DocumentAt(index)
			if !present {
				kind = recovery.ActionKindError
			} else if document.Restoration().Changed() {
				kind = recovery.ActionKindRestoreWrite
				if !document.Restoration().Document().Exists() {
					kind = recovery.ActionKindRestoreDelete
				}
			}
		}
		result = append(result, NativeRecoveryDocumentAction{Address: group[0], Kind: kind})
	}
	return result
}
