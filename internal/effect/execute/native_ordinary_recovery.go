package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/registrysettlement"

	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

type nativeOrdinaryRecoveryStage struct {
	actions  []recoveryHostAction
	rollback hostRollback
	plan     recoveryHostExecutionPlan
}

func prepareNativeOrdinaryRecovery(ctx context.Context, authority *mutationAuthority, operation *journal.NativeRecoveryOperation, actions []recoveryHostAction) (nativeOrdinaryRecoveryStage, error) {
	plan := operation.Plan()
	if len(actions) == 0 {
		return nativeOrdinaryRecoveryStage{}, nil
	}
	if err := authority.setJournalExecutionBasis(plan.RecordFingerprint(), plan.ActiveJournalAuthority()); err != nil {
		return nativeOrdinaryRecoveryStage{}, err
	}
	for _, action := range actions {
		destination, err := recoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return nativeOrdinaryRecoveryStage{}, err
		}
		if err := authority.destinations.Bind(action.Scope, destination); err != nil {
			return nativeOrdinaryRecoveryStage{}, err
		}
	}
	if err := authority.prepareRecoveryBackups(ctx, plan.OperationDir(), actions); err != nil {
		return nativeOrdinaryRecoveryStage{}, err
	}
	rollback, err := stageRecoveryRollback(ctx, authority, actions, operation.Codecs())
	if err != nil {
		return nativeOrdinaryRecoveryStage{}, err
	}
	hostPlan, err := prepareRecoveryHostExecutionPlan(authority, actions)
	if err != nil {
		return nativeOrdinaryRecoveryStage{}, errors.Join(err, rollback.cleanup(context.WithoutCancel(ctx), authority))
	}
	return nativeOrdinaryRecoveryStage{actions: actions, rollback: rollback, plan: hostPlan}, nil
}

func (stage nativeOrdinaryRecoveryStage) cleanup(ctx context.Context, authority *mutationAuthority) error {
	if len(stage.actions) == 0 {
		return nil
	}
	return stage.rollback.cleanup(ctx, authority)
}

func (stage nativeOrdinaryRecoveryStage) execute(ctx context.Context, authority *mutationAuthority, operation *journal.NativeRecoveryOperation, store ownershipmutation.RegistryStore, semanticGuard func(context.Context) error, gate visibilityEffectGate) (resultErr error) {
	if len(stage.actions) == 0 {
		return nil
	}
	releaseCheck, err := authority.control.InstallNativeCheck(semanticGuard)
	if err != nil {
		return err
	}
	defer releaseCheck()
	intents, err := operation.Plan().OrdinaryProvisionalAcquires()
	if err != nil {
		return err
	}
	reader := func(ctx context.Context) (ownership.Registry, error) {
		return registrysettlement.Read(ctx, operation, operation.InitialAssessment(), store)
	}
	visit := 0
	beforeAction := func(index int) error {
		if visit >= len(stage.plan.visitOrder) || stage.plan.visitOrder[visit].index != index {
			return fmt.Errorf("ordinary restoration differs from its exact compiled visit order")
		}
		visit++
		return semanticGuard(ctx)
	}
	err = executeRecoveryHostActionPlan(ctx, authority, stage.actions, stage.rollback.entries, stage.plan, beforeAction, recoveryIntentClaimGuardWithReader(intents, reader, authority), operation.Codecs(), gate)
	if err != nil {
		return err
	}
	if visit != len(stage.plan.visitOrder) {
		return fmt.Errorf("ordinary restoration did not consume its complete compiled visit order")
	}
	return nil
}

func (authority *mutationAuthority) beginJointGeneralRecoveryExecution() error {
	if authority == nil || authority.physicalWorkBudget == nil || authority.generalExecutionWorkBudget != nil {
		return fmt.Errorf("joint recovery requires its original unused host execution allocation")
	}
	host, err := authority.physicalWorkBudget.BeginGeneralExecutionRetainingControl()
	if err != nil {
		return err
	}
	if err := authority.generalTraversalPhase.Advance(authority.physicalWorkBudget, host); err != nil {
		return err
	}
	authority.generalExecutionWorkBudget = host
	return nil
}
