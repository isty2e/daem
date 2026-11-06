package hostmutation

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/controlwitness"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type Restoration struct {
	*restorationState
}

type restorationState struct {
	operation *Operation
	control   *controlwitness.Guard
	sources   *Backups
	original  *physicalwork.Budget
	codecs    aggregate.CodecCatalog
	actions   []RestorationAction
	rollback  hostRollback
	plan      *recoveryHostExecutionPlan
}

func (operation *Operation) StageRestoration(
	ctx context.Context,
	original *physicalwork.Budget,
	control *controlwitness.Guard,
	sources *Backups,
	actions []RestorationAction,
	codecs aggregate.CodecCatalog,
) (*Restoration, error) {
	ownedActions := slices.Clone(actions)
	for index := range ownedActions {
		ownedActions[index] = ownedActions[index].clone()
	}
	restoration := &Restoration{restorationState: &restorationState{
		operation: operation, control: control, sources: sources,
		original: original, codecs: codecs, actions: ownedActions,
	}}
	rollback, err := stageRecoveryRollback(ctx, restoration, ownedActions, codecs)
	if err != nil {
		return nil, err
	}
	restoration.rollback = rollback
	return restoration, nil
}

func (restoration *Restoration) PrepareExecution() ([]int, error) {
	if restoration == nil || restoration.restorationState == nil {
		return nil, fmt.Errorf("recovery restoration stage is unavailable")
	}
	if restoration.plan == nil {
		plan, err := prepareRecoveryHostExecutionPlan(restoration, restoration.actions)
		if err != nil {
			return nil, err
		}
		restoration.plan = &plan
	}
	return slices.Clone(restoration.plan.visitOrder), nil
}

func (restoration *Restoration) Execute(
	ctx context.Context,
	budget *physicalwork.Budget,
	beforeAction func(int) error,
	ownershipGuard RestorationOwnershipGuard,
	gate applyprogram.VisibilityGate,
) error {
	if restoration == nil || restoration.restorationState == nil || restoration.plan == nil {
		return fmt.Errorf("recovery restoration execution plan is unavailable")
	}
	if budget == nil {
		return fmt.Errorf("recovery restoration requires its selected execution allocation")
	}
	return executeRecoveryHostActionPlan(
		ctx, restoration, budget, restoration.actions, restoration.rollback.entries,
		*restoration.plan, beforeAction, ownershipGuard, restoration.codecs, gate,
	)
}

func (restoration *Restoration) Restore(
	ctx context.Context,
	budget *physicalwork.Budget,
	gate applyprogram.VisibilityGate,
) error {
	if restoration == nil || restoration.restorationState == nil {
		return nil
	}
	if budget == nil {
		return fmt.Errorf("recovery restoration requires its selected execution allocation")
	}
	return restoration.rollback.restore(ctx, restoration, budget, gate)
}

func (restoration *Restoration) Cleanup(ctx context.Context) error {
	if restoration == nil || restoration.restorationState == nil {
		return nil
	}
	return restoration.rollback.scratch.Cleanup(ctx)
}
