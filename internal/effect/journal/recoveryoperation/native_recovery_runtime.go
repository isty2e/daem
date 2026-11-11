package recoveryoperation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output/ownership"
)

func (operation *NativeRecoveryOperation) InitialAssessment() journalqualification.NativeRecoveryAssessment {
	if operation == nil || operation.closed {
		return journalqualification.NativeRecoveryAssessment{}
	}
	return operation.initial
}

func (operation *NativeRecoveryOperation) ObservedState() durable.Snapshot {
	state, _ := operation.plan.assessment.Persistence()
	return state
}

func (operation *NativeRecoveryOperation) ObservedRegistry() ownership.Registry {
	_, registry := operation.plan.assessment.Persistence()
	return registry
}

func (operation *NativeRecoveryOperation) Assess(ctx context.Context, options journalqualification.NativeRecoveryOptions) (journalqualification.NativeRecoveryAssessment, error) {
	if operation == nil || operation.closed {
		return journalqualification.NativeRecoveryAssessment{}, fmt.Errorf("live native recovery operation is required")
	}
	if operation.captured != nil {
		return operation.captured.AssessRecovery(ctx, options)
	}
	return journalqualification.ReassessNativeSource(ctx, operation.plan.source, journalqualification.NativeReadOptions{
		Documents: journalqualification.ReadOptions{Protection: operation.profile.Protection, Rules: journalqualification.RecordedRuleExpectations(), Filesystem: options.Filesystem, Budget: operation.profile.Budget, Limits: operation.profile.Limits, Codecs: options.Codecs, OrdinaryCapabilities: operation.ordinaryCapabilities},
		Roots:     operation.roots, State: options.State, Registry: options.Registry, Resolver: operation.resolver,
		RawRegistry: operation.profile.RawRegistry, RecoveryDemand: operation.demand,
	}, options.OperationAuthority)
}

func (operation *NativeRecoveryOperation) PrepareRetirement(ctx context.Context, filesystem mutationfs.RootedStore) (*NativeJournalRetirement, error) {
	if operation == nil || operation.closed {
		return nil, fmt.Errorf("live native recovery operation is required")
	}
	retiring, err := prepareNativeRetirement(ctx, operation.plan.capture, operation.plan.active, operation.profile.Budget, filesystem, operation.profile.StateCodec)
	if err == nil && operation.inventoryProfile.decoder != nil {
		retiring.prepared.journalDecoder = operation.inventoryProfile.decoder
	}
	return retiring, err
}
