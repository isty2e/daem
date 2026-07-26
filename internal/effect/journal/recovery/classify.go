package recovery

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

// Classify derives one recovery plan from complete durable authority, an
// opaque selected subset, and fresh normalized evidence.
func Classify(
	authority Authority,
	selection Selection,
	currentState durable.Snapshot,
	pathEvidence []PathEvidence,
	backupEvidence []BackupEvidence,
	registry ownership.Registry,
) (Plan, error) {
	if err := selection.validate(authority); err != nil {
		return Plan{}, err
	}
	pathsByEntry, err := pathEvidenceIndex(pathEvidence)
	if err != nil {
		return Plan{}, err
	}
	backupsByPath, err := backupEvidenceIndex(backupEvidence)
	if err != nil {
		return Plan{}, err
	}

	stateBefore := currentState.Equal(authority.statefileBefore)
	stateAfter := currentState.Equal(authority.statefileAfter)
	claimsBefore, claimsPrepared, claimsAfter, claimsRollbackEligible, claimsFinalizeEligible := classifyClaimTransitions(authority.claimTransitions, registry)
	pathsBefore := true
	pathsAfter := true
	pathsRecoverable := true
	actions := make([]Action, 0, len(selection.indexes))

	for _, entryIndex := range selection.indexes {
		entry := authority.entries[entryIndex]
		observation, ok := pathsByEntry[pathEvidenceKey{path: entry.destination, contentPath: entry.contentPath}]
		action := actionFromEntry(entry)
		if !ok {
			action.Kind = ActionKindError
			action.Reason = "observation_error"
			action.Detail = "path observation is required"
			actions = append(actions, action)
			pathsBefore = false
			pathsAfter = false
			pathsRecoverable = false
			continue
		}
		if observation.Error != "" {
			action.Kind = ActionKindError
			action.Reason = "observation_error"
			action.Detail = observation.Error
			actions = append(actions, action)
			pathsBefore = false
			pathsAfter = false
			pathsRecoverable = false
			continue
		}

		before := pathMatchesBefore(entry.before, observation)
		after := pathMatchesExpected(entry.expectedAfter, observation)
		if !before {
			pathsBefore = false
		}
		if !after {
			pathsAfter = false
		}
		if !before && !after {
			action.Kind = ActionKindError
			action.Reason = "blocked"
			action.Detail = "path differs from both before and expected-after states"
			actions = append(actions, action)
			pathsRecoverable = false
			continue
		}
		if before {
			action.Kind = ActionKindNoOp
			action.Reason = "already_before"
			actions = append(actions, action)
			continue
		}

		if entry.before.Existed && isContentBackedPathKind(entry.before.Kind) {
			if detail := backupMismatch(entry.before, backupsByPath); detail != "" {
				action.Kind = ActionKindError
				action.Reason = "backup_mismatch"
				action.Detail = detail
				actions = append(actions, action)
				pathsRecoverable = false
				continue
			}
			action.Kind = ActionKindRestoreWrite
			action.Reason = "restore_" + entry.before.Kind
			action.BackupPath = entry.before.BackupPath
			action.BackupHash = entry.before.ContentHash
			action.BackupKind = entry.before.Kind
		} else if entry.before.Existed {
			action.Kind = ActionKindError
			action.Reason = "unsupported_before_state"
			action.Detail = fmt.Sprintf("before path kind %q is not supported", entry.before.Kind)
			pathsRecoverable = false
		} else {
			action.Kind = ActionKindRestoreDelete
			action.Reason = "restore_absent"
		}
		actions = append(actions, action)
	}

	if pathsBefore && stateBefore && claimsBefore {
		return newPlan(authority, ClassificationCleanBefore,
			[]Action{cleanupAction(authority.operationID, ClassificationCleanBefore)}, actions), nil
	}
	if pathsAfter && stateAfter && claimsAfter {
		return newPlan(authority, ClassificationCleanAfter,
			[]Action{cleanupAction(authority.operationID, ClassificationCleanAfter)}, actions), nil
	}
	if pathsAfter && stateAfter && claimsFinalizeEligible && !claimsAfter {
		return newPlan(authority, ClassificationNeedsFinalize,
			[]Action{{Kind: ActionKindFinalizeClaims, Reason: string(ClassificationNeedsFinalize), Destination: authority.operationID}}, actions), nil
	}
	if pathsRecoverable && stateBefore && claimsRollbackEligible {
		return newPlan(authority, ClassificationNeedsRollback, actions, actions), nil
	}

	if !stateBefore && !stateAfter {
		actions = append(actions, Action{
			Kind: ActionKindError, Reason: "state_mismatch",
			Detail: "statefile differs from both before and expected-after states",
		})
	} else if stateAfter && !pathsAfter {
		actions = append(actions, Action{
			Kind: ActionKindError, Reason: "state_mismatch",
			Detail: "statefile is after apply but host paths are not clean_after",
		})
	}
	if !claimsBefore && !claimsPrepared && !claimsAfter {
		actions = append(actions, Action{Kind: ActionKindError, Reason: "claim_mismatch", Detail: "ownership claims differ from before, prepared, and expected-after phases"})
	} else if stateBefore && !claimsRollbackEligible {
		actions = append(actions, Action{Kind: ActionKindError, Reason: "claim_mismatch", Detail: "ownership claims cannot be rolled back from the observed phase"})
	} else if stateAfter && !claimsFinalizeEligible {
		actions = append(actions, Action{Kind: ActionKindError, Reason: "claim_mismatch", Detail: "ownership claims cannot be finalized from the observed phase"})
	}

	return newPlan(authority, ClassificationBlocked, actions, actions), nil
}

func newPlan(authority Authority, classification Classification, actions []Action, guarded []Action) Plan {
	return Plan{
		authority:      authority,
		classification: classification,
		actions:        cloneActions(actions),
		guardedActions: cloneActions(guarded),
	}
}

func cleanupAction(operationID string, classification Classification) Action {
	return Action{Kind: ActionKindCleanup, Reason: string(classification), Destination: operationID}
}

func actionFromEntry(entry Entry) Action {
	action := Action{
		subject:             entry.subject,
		Target:              entry.target,
		ConsumerTargets:     append([]target.Target(nil), entry.consumerTargets...),
		Scope:               entry.scope,
		Destination:         entry.destination,
		ContentPath:         entry.contentPath,
		ContentKind:         entry.contentKind,
		BeforePathMode:      clonePermissionMode(entry.before.PathMode),
		BeforePathExisted:   entry.before.PathExisted,
		BeforeParentExisted: entry.before.ParentExisted,
		ExpectedAfter:       entry.expectedAfter.Clone(),
	}
	if entry.aggregateContract != nil {
		contract := entry.aggregateContract.Clone()
		action.AggregateContract = &contract
	}
	return action
}
