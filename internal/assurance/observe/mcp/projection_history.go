package mcp

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func projectionOwnership(
	contract lock.LockedSubjectContract,
	contribution aggregate.ManagedContribution,
	currentState durable.Snapshot,
	projectionPresent bool,
) (OwnershipState, error) {
	if stateOwnsProjection(contract, contribution, currentState) {
		if contract.Ownership() == lock.OwnershipAdopted {
			return OwnershipAdopted, nil
		}
		return OwnershipManaged, nil
	}
	if projectionPresent {
		return OwnershipUnmanagedSameName, nil
	}
	return OwnershipUnknown, nil
}

func stateOwnsProjection(
	contract lock.LockedSubjectContract,
	contribution aggregate.ManagedContribution,
	currentState durable.Snapshot,
) bool {
	subject := contract.SubjectID()
	for _, state := range currentState.ManagedAggregates() {
		if state.Subject() == subject {
			return state.Contribution().Address() == contribution.Address()
		}
	}
	return false
}

func lastDelegateAttempt(
	contract lock.LockedSubjectContract,
	contribution aggregate.ManagedContribution,
	currentState durable.Snapshot,
) (LastDelegateAttemptInput, error) {
	delegatePlan, ok := contract.DelegatePlan()
	if !ok {
		return LastDelegateAttemptInput{}, nil
	}
	recordedAttempt, ok := findDelegateAttemptRecord(
		currentState.DelegateAttempts(),
		contract.SubjectID(),
		contribution.Target(),
		contribution.Scope(),
	)
	if !ok {
		return LastDelegateAttemptInput{}, nil
	}
	if !recordedAttempt.MatchesPlanIdentity(delegatePlan.IdentityKey()) {
		return LastDelegateAttemptInput{
			Observed:            true,
			MatchesPlanIdentity: false,
		}, nil
	}
	return LastDelegateAttemptInput{
		Observed:            true,
		MatchesPlanIdentity: true,
		Status:              delegateAttemptState(recordedAttempt.Status()),
		Reason:              delegateAttemptReason(recordedAttempt.Reason()),
	}, nil
}

func findDelegateAttemptRecord(
	records []durableattempt.DelegateAttempt,
	subject topology.SubjectID,
	selectedTarget target.Target,
	selectedScope target.Scope,
) (durableattempt.DelegateAttempt, bool) {
	for _, item := range records {
		if item.Subject() == subject &&
			item.Target() == selectedTarget &&
			item.Scope() == selectedScope {
			return item, true
		}
	}
	return durableattempt.DelegateAttempt{}, false
}

func delegateAttemptState(status durableattempt.DelegateAttemptStatus) DelegateAttemptState {
	switch status {
	case durableattempt.DelegateStatusSucceeded:
		return DelegateAttemptSucceeded
	case durableattempt.DelegateStatusFailed:
		return DelegateAttemptFailed
	case durableattempt.DelegateStatusBlocked:
		return DelegateAttemptBlocked
	default:
		return DelegateAttemptNotObserved
	}
}

func delegateAttemptReason(reason durableattempt.DelegateAttemptReason) ReasonCode {
	switch reason {
	case durableattempt.DelegateReasonPolicyBlocked:
		return ReasonDelegatePolicyBlocked
	case durableattempt.DelegateReasonMissingEnvRef:
		return ReasonDelegateMissingEnvRef
	case durableattempt.DelegateReasonMissingRunner:
		return ReasonDelegateMissingRunner
	case durableattempt.DelegateReasonNonZeroExit:
		return ReasonDelegateNonZeroExit
	case durableattempt.DelegateReasonTimeout:
		return ReasonDelegateTimeout
	case durableattempt.DelegateReasonRunnerError:
		return ReasonDelegateRunnerError
	case durableattempt.DelegateReasonWorkDirAuthority:
		return ReasonDelegateWorkDirAuthority
	default:
		return ReasonNone
	}
}
