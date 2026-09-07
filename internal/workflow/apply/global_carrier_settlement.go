package apply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
)

type globalCarrierSettlementKind uint8

const (
	globalCarrierSettlementRetirement globalCarrierSettlementKind = iota + 1
	globalCarrierSettlementPromotion
	globalCarrierSettlementAdoption
)

type globalCarrierSettlementPlan struct {
	kind         globalCarrierSettlementKind
	registryPath string
	baseline     durablecarrier.GlobalCarrierClaims
	claims       []durablecarrier.ManagedCarrierClaim
	action       reconcile.RelationAction
	claim        durablecarrier.ManagedCarrierClaim
	actionPlan   mutation.OperationFingerprint
	ref          string
	structure    operationplan.EffectStructure
	valid        bool
}

func newGlobalCarrierBatchSettlementPlan(
	kind globalCarrierSettlementKind,
	registryPath string,
	baseline durablecarrier.GlobalCarrierClaims,
	claims []durablecarrier.ManagedCarrierClaim,
) (globalCarrierSettlementPlan, error) {
	if kind != globalCarrierSettlementRetirement && kind != globalCarrierSettlementAdoption {
		return globalCarrierSettlementPlan{}, fmt.Errorf("global carrier batch settlement kind %d is invalid", kind)
	}
	ref := "apply/global-carrier-retirement"
	if kind == globalCarrierSettlementAdoption {
		ref = "apply/global-carrier-adoption"
	}
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.ForwardPhase(
		ref,
		compileApplyGlobalCarrierBatchSettlementSchedule(&builder, ref),
	))
	if err != nil {
		return globalCarrierSettlementPlan{}, err
	}
	return globalCarrierSettlementPlan{
		kind:         kind,
		registryPath: registryPath,
		baseline:     baseline,
		claims:       append([]durablecarrier.ManagedCarrierClaim(nil), claims...),
		ref:          ref,
		structure:    structure,
		valid:        true,
	}, nil
}

func newGlobalCarrierPromotionSettlementPlan(
	registryPath string,
	baseline durablecarrier.GlobalCarrierClaims,
	action reconcile.RelationAction,
	claim durablecarrier.ManagedCarrierClaim,
) (globalCarrierSettlementPlan, error) {
	const ref = "apply/global-carrier-promotion"
	actionPlan, err := globalCarrierPromotionFingerprint(action)
	if err != nil {
		return globalCarrierSettlementPlan{}, err
	}
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(
		compileApplyGlobalCarrierPromotionSettlementSchedule(
			&builder,
			ref,
			compileApplyCheckedStep(
				&builder,
				ref+"/statefile/pre-registry",
				operationplan.EffectStepValidateDescendant,
			),
			compileApplyCheckedStep(
				&builder,
				ref+"/statefile/post-registry",
				operationplan.EffectStepValidateDescendant,
			),
			compileApplyCheckedStep(
				&builder,
				ref+"/statefile/project-claim",
				operationplan.EffectStepPublishDescendant,
			),
			compileApplyCheckedStep(
				&builder,
				ref+"/statefile/post-claim",
				operationplan.EffectStepValidateDescendant,
			),
		),
	)
	if err != nil {
		return globalCarrierSettlementPlan{}, err
	}
	return globalCarrierSettlementPlan{
		kind:         globalCarrierSettlementPromotion,
		registryPath: registryPath,
		baseline:     baseline,
		action:       action,
		claim:        claim,
		actionPlan:   actionPlan,
		ref:          ref,
		structure:    structure,
		valid:        true,
	}, nil
}

func (plan globalCarrierSettlementPlan) beginBatch(
	kind globalCarrierSettlementKind,
	registryPath string,
	baseline durablecarrier.GlobalCarrierClaims,
	claims []durablecarrier.ManagedCarrierClaim,
) (*globalCarrierSettlementExecution, error) {
	if !plan.valid || plan.kind != kind {
		return nil, fmt.Errorf("global carrier settlement plan kind does not match the requested batch")
	}
	if plan.registryPath != registryPath {
		return nil, fmt.Errorf("global carrier settlement registry path changed")
	}
	if !plan.baseline.Equal(baseline) {
		return nil, fmt.Errorf("global carrier settlement registry baseline changed")
	}
	if !exactGlobalCarrierClaimSequence(plan.claims, claims) {
		return nil, fmt.Errorf("global carrier settlement claim facts changed")
	}
	return &globalCarrierSettlementExecution{
		plan:   plan,
		cursor: plan.structure.Begin(),
	}, nil
}

func (plan globalCarrierSettlementPlan) beginPromotion(
	registryPath string,
	baseline durablecarrier.GlobalCarrierClaims,
	action reconcile.RelationAction,
	claim durablecarrier.ManagedCarrierClaim,
) (*globalCarrierSettlementExecution, error) {
	if !plan.valid || plan.kind != globalCarrierSettlementPromotion {
		return nil, fmt.Errorf("global carrier settlement plan is not a promotion")
	}
	if plan.registryPath != registryPath {
		return nil, fmt.Errorf("global carrier settlement registry path changed")
	}
	if !plan.baseline.Equal(baseline) {
		return nil, fmt.Errorf("global carrier settlement registry baseline changed")
	}
	actionPlan, err := globalCarrierPromotionFingerprint(action)
	if err != nil {
		return nil, err
	}
	if plan.action.Compare(action) != 0 || !plan.actionPlan.Equal(actionPlan) || !plan.claim.ExactEqual(claim) {
		return nil, fmt.Errorf("global carrier settlement promotion facts changed")
	}
	return &globalCarrierSettlementExecution{
		plan:   plan,
		cursor: plan.structure.Begin(),
	}, nil
}

func globalCarrierPromotionFingerprint(
	action reconcile.RelationAction,
) (mutation.OperationFingerprint, error) {
	payload, err := json.Marshal(relationFingerprintRows([]reconcile.RelationAction{action}))
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf(
			"fingerprint global carrier promotion: %w",
			err,
		)
	}
	return mutation.NewOperationFingerprint(payload), nil
}

func exactGlobalCarrierClaimSequence(
	left []durablecarrier.ManagedCarrierClaim,
	right []durablecarrier.ManagedCarrierClaim,
) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].ExactEqual(right[index]) {
			return false
		}
	}
	return true
}

type globalCarrierSettlementExecution struct {
	plan     globalCarrierSettlementPlan
	cursor   *operationplan.EffectCursor
	terminal bool
	finished bool
}

func (execution *globalCarrierSettlementExecution) checked(
	ctx context.Context,
	ref string,
	kind operationplan.EffectStepKind,
	call func() error,
) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("global carrier settlement execution is unavailable")
	}
	if ctx == nil {
		return fmt.Errorf("global carrier settlement context is required")
	}
	var consumeErr error
	if kind == operationplan.EffectStepForwardEffect {
		_, consumeErr = execution.cursor.ConsumeForwardEffect(ref)
	} else {
		consumeErr = execution.cursor.Consume(ref, kind)
	}
	if consumeErr != nil {
		return consumeErr
	}
	callErr := ctx.Err()
	if callErr == nil {
		callErr = call()
	}
	settleErr := execution.settle(ref+"/outcome", callErr)
	return errors.Join(callErr, settleErr)
}

func (execution *globalCarrierSettlementExecution) settle(ref string, cause error) error {
	alternative := 0
	stepID := ref + "/success"
	kind := operationplan.EffectStepNoOp
	if cause != nil {
		alternative = 1
		stepID = ref + "/failure"
		kind = operationplan.EffectStepTerminal
	}
	if err := execution.cursor.SelectAlternative(ref, alternative); err != nil {
		return err
	}
	if err := execution.cursor.Consume(stepID, kind); err != nil {
		return err
	}
	if cause != nil {
		execution.terminal = true
	}
	return nil
}

func (execution *globalCarrierSettlementExecution) finish(resultErr error) error {
	if execution == nil || execution.cursor == nil {
		return errors.Join(resultErr, fmt.Errorf("global carrier settlement execution is unavailable"))
	}
	if execution.finished {
		return errors.Join(resultErr, fmt.Errorf("global carrier settlement execution is already finished"))
	}
	var finishErr error
	if execution.terminal || resultErr == nil {
		finishErr = execution.cursor.FinishSuccess()
	} else {
		finishErr = execution.cursor.AbortBeforeEffect()
	}
	if finishErr == nil {
		execution.finished = true
	}
	return errors.Join(resultErr, finishErr)
}

func globalCarrierClaimsAfterPersistence(
	current durablecarrier.GlobalCarrierClaims,
	successor durablecarrier.GlobalCarrierClaims,
	observed durablecarrier.GlobalCarrierClaims,
	err error,
) (durablecarrier.GlobalCarrierClaims, error) {
	if err == nil {
		return observed, nil
	}
	kind, classified := mutationfs.FailureKindOf(err)
	if classified && (kind == mutationfs.FailureIndeterminateCommit || kind == mutationfs.FailureRetainedResidue) {
		return successor, err
	}
	return current, err
}

type globalCarrierBatchSettlementCallbacks struct {
	validateBefore func() error
	persist        func() (durablecarrier.GlobalCarrierClaims, int, error)
	validateAfter  func() error
}

func executeGlobalCarrierBatchSettlement(
	ctx context.Context,
	plan globalCarrierSettlementPlan,
	kind globalCarrierSettlementKind,
	registryPath string,
	claims []durablecarrier.ManagedCarrierClaim,
	current durablecarrier.GlobalCarrierClaims,
	callbacks globalCarrierBatchSettlementCallbacks,
) (durablecarrier.GlobalCarrierClaims, int, error) {
	if len(claims) == 0 {
		return current, 0, nil
	}
	if callbacks.validateBefore == nil || callbacks.persist == nil || callbacks.validateAfter == nil {
		return current, 0, fmt.Errorf("global carrier batch settlement callbacks are incomplete")
	}
	execution, err := plan.beginBatch(kind, registryPath, current, claims)
	if err != nil {
		return current, 0, err
	}
	if err := execution.checked(
		ctx,
		plan.ref+"/pre-registry",
		operationplan.EffectStepForwardEffect,
		callbacks.validateBefore,
	); err != nil {
		return current, 0, execution.finish(err)
	}
	next := current
	count := 0
	if err := execution.checked(
		ctx,
		plan.ref+"/persistence",
		operationplan.EffectStepPersistence,
		func() error {
			var persistErr error
			next, count, persistErr = callbacks.persist()
			return persistErr
		},
	); err != nil {
		return next, count, execution.finish(err)
	}
	if err := execution.checked(
		ctx,
		plan.ref+"/post-registry",
		operationplan.EffectStepForwardEffect,
		callbacks.validateAfter,
	); err != nil {
		return next, count, execution.finish(err)
	}
	return next, count, execution.finish(nil)
}

type globalCarrierPromotionSettlementCallbacks struct {
	validateDeclarationsBefore func() error
	validateProjectRootBefore  func() error
	validateStatefileBefore    func() error
	persistRegistry            func() (durablecarrier.GlobalCarrierClaims, error)
	validateStatefileAfter     func() error
	acceptRegistryVisibility   func() error
	publishStatefile           func(durablecarrier.GlobalCarrierClaims) (durable.Snapshot, error)
	validateStatefileFinal     func() error
	acceptStatefileVisibility  func() error
	validateProjectRootAfter   func() error
	validateDeclarationsAfter  func() error
}

func executeGlobalCarrierPromotionSettlement(
	ctx context.Context,
	plan globalCarrierSettlementPlan,
	registryPath string,
	action reconcile.RelationAction,
	claim durablecarrier.ManagedCarrierClaim,
	current durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	callbacks globalCarrierPromotionSettlementCallbacks,
) (durable.Snapshot, durablecarrier.GlobalCarrierClaims, error) {
	if callbacks.validateDeclarationsBefore == nil ||
		callbacks.validateProjectRootBefore == nil ||
		callbacks.validateStatefileBefore == nil ||
		callbacks.persistRegistry == nil ||
		callbacks.validateStatefileAfter == nil ||
		callbacks.acceptRegistryVisibility == nil ||
		callbacks.publishStatefile == nil ||
		callbacks.validateStatefileFinal == nil ||
		callbacks.acceptStatefileVisibility == nil ||
		callbacks.validateProjectRootAfter == nil ||
		callbacks.validateDeclarationsAfter == nil {
		return current, registry, fmt.Errorf("global carrier promotion settlement callbacks are incomplete")
	}
	execution, err := plan.beginPromotion(registryPath, registry, action, claim)
	if err != nil {
		return current, registry, err
	}
	checked := func(ref string, kind operationplan.EffectStepKind, call func() error) error {
		return execution.checked(ctx, plan.ref+ref, kind, call)
	}
	if err := checked(
		"/declarations-before-registry",
		operationplan.EffectStepObservation,
		callbacks.validateDeclarationsBefore,
	); err != nil {
		return current, registry, execution.finish(err)
	}
	if err := checked(
		"/project-root-before-registry",
		operationplan.EffectStepObservation,
		callbacks.validateProjectRootBefore,
	); err != nil {
		return current, registry, execution.finish(err)
	}
	if err := checked(
		"/statefile/pre-registry",
		operationplan.EffectStepValidateDescendant,
		callbacks.validateStatefileBefore,
	); err != nil {
		return current, registry, execution.finish(err)
	}
	nextRegistry := registry
	if err := checked(
		"/global-registry",
		operationplan.EffectStepPersistence,
		func() error {
			var persistErr error
			nextRegistry, persistErr = callbacks.persistRegistry()
			return persistErr
		},
	); err != nil {
		return current, nextRegistry, execution.finish(err)
	}
	if err := checked(
		"/statefile/post-registry",
		operationplan.EffectStepValidateDescendant,
		callbacks.validateStatefileAfter,
	); err != nil {
		return current, nextRegistry, execution.finish(err)
	}
	if err := checked(
		"/registry-visibility",
		operationplan.EffectStepObservation,
		callbacks.acceptRegistryVisibility,
	); err != nil {
		return current, nextRegistry, execution.finish(err)
	}
	nextState := current
	if err := checked(
		"/statefile/project-claim",
		operationplan.EffectStepPublishDescendant,
		func() error {
			candidate, publishErr := callbacks.publishStatefile(nextRegistry)
			if publishErr == nil {
				nextState = candidate
			}
			return publishErr
		},
	); err != nil {
		return nextState, nextRegistry, execution.finish(err)
	}
	if err := checked(
		"/statefile/post-claim",
		operationplan.EffectStepValidateDescendant,
		callbacks.validateStatefileFinal,
	); err != nil {
		return nextState, nextRegistry, execution.finish(err)
	}
	if err := checked(
		"/statefile-visibility",
		operationplan.EffectStepObservation,
		callbacks.acceptStatefileVisibility,
	); err != nil {
		return nextState, nextRegistry, execution.finish(err)
	}
	if err := checked(
		"/project-root-after-claim",
		operationplan.EffectStepObservation,
		callbacks.validateProjectRootAfter,
	); err != nil {
		return nextState, nextRegistry, execution.finish(err)
	}
	if err := checked(
		"/declarations-after-claim",
		operationplan.EffectStepObservation,
		callbacks.validateDeclarationsAfter,
	); err != nil {
		return nextState, nextRegistry, execution.finish(err)
	}
	return nextState, nextRegistry, execution.finish(nil)
}
