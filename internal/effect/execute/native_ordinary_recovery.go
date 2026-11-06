package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"

	"github.com/isty2e/daem/internal/effect/execute/registrysettlement"

	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

type nativeOrdinaryRecoveryStage struct {
	restoration *hostmutation.Restoration
	visits      []int
}

func prepareNativeOrdinaryRecovery(ctx context.Context, authority *mutationAuthority, operation *journal.NativeRecoveryOperation, actions []hostmutation.RestorationAction) (nativeOrdinaryRecoveryStage, error) {
	plan := operation.Plan()
	if len(actions) == 0 {
		return nativeOrdinaryRecoveryStage{}, nil
	}
	if err := authority.setJournalExecutionBasis(plan.RecordFingerprint(), plan.ActiveJournalAuthority()); err != nil {
		return nativeOrdinaryRecoveryStage{}, err
	}
	for _, action := range actions {
		destination, err := hostmutation.ParseRestorationDestination(action.Scope, action.Destination)
		if err != nil {
			return nativeOrdinaryRecoveryStage{}, err
		}
		if err := authority.destinations.Bind(action.Scope, destination); err != nil {
			return nativeOrdinaryRecoveryStage{}, err
		}
	}
	if err := authority.hostBackups().Prepare(ctx, plan.OperationDir(), hostmutation.RestorationBackupRequests(actions), authority.journalBasis.activeAuthority); err != nil {
		return nativeOrdinaryRecoveryStage{}, err
	}
	restoration, err := authority.hostMutation().StageRestoration(ctx, authority.physicalWorkBudget, authority.control, authority.hostBackups(), actions, operation.Codecs())
	if err != nil {
		return nativeOrdinaryRecoveryStage{}, err
	}
	visits, err := restoration.PrepareExecution()
	if err != nil {
		return nativeOrdinaryRecoveryStage{}, errors.Join(err, restoration.Cleanup(context.WithoutCancel(ctx)))
	}
	return nativeOrdinaryRecoveryStage{restoration: restoration, visits: visits}, nil
}

func (stage nativeOrdinaryRecoveryStage) execute(ctx context.Context, authority *mutationAuthority, operation *journal.NativeRecoveryOperation, store ownershipmutation.RegistryStore, semanticGuard func(context.Context) error, gate applyprogram.VisibilityGate) (resultErr error) {
	if stage.restoration == nil {
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
		if visit >= len(stage.visits) || stage.visits[visit] != index {
			return fmt.Errorf("ordinary restoration differs from its exact compiled visit order")
		}
		visit++
		return semanticGuard(ctx)
	}
	err = stage.restoration.Execute(ctx, authority.generalExecutionWorkBudget, beforeAction, recoveryIntentClaimGuardWithReader(intents, reader, authority), gate)
	if err != nil {
		return err
	}
	if visit != len(stage.visits) {
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
