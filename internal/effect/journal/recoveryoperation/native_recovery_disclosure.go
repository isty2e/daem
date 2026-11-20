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

// DisclosedClassification preserves the native recovery v11 heading even when
// only cleanup, rather than the interrupted operation, is blocked.
func (plan NativeRecoveryPlan) DisclosedClassification() recovery.Classification {
	if plan.Blocked() {
		return recovery.ClassificationBlocked
	}
	return plan.assessment.Classification()
}

// DisclosedOrdinaryActions hides restoration rows on a hard refusal but keeps
// them when cleanup could not be observed and can be retried.
func (plan NativeRecoveryPlan) DisclosedOrdinaryActions() []recovery.Action {
	if plan.Blocked() {
		return nil
	}
	return plan.OrdinaryRollbackActions()
}

func (plan NativeRecoveryPlan) DocumentActions() []NativeRecoveryDocumentAction {
	aliases, err := plan.DocumentAliases()
	if err != nil {
		return nil
	}
	result := make([]NativeRecoveryDocumentAction, 0, len(aliases))
	blocked := plan.Blocked()
	for index, group := range aliases {
		if len(group) == 0 {
			continue
		}
		kind := recovery.ActionKindNoOp
		if blocked {
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
