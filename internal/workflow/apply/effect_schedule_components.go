package apply

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

type applyScheduleInput struct {
	providerRoutes      []applyRouteScheduleFact
	coreChanged         bool
	core                operationplan.EffectNode
	hasGlobalRetirement bool
	carrierRemovals     []applyCarrierScheduleFact
	finalRoutes         []applyRouteScheduleFact
	orderClasses        []applyOrderScheduleFact
	mayReclassifyOrder  bool
	delegates           []applyDelegateScheduleFact
	hasGlobalAdoption   bool
}

type applyRouteScheduleFact struct {
	ref               string
	action            reconcile.RelationAction
	work              operationplan.RouteWork
	preflightRejected bool
}

type applyCarrierScheduleFact struct {
	ref         string
	action      carrierabsence.Action
	fingerprint mutation.OperationFingerprint
	work        operationplan.CarrierWork
	mode        applyCarrierScheduleMode
	scope       target.Scope
}

type applyCarrierScheduleMode uint8

const (
	applyCarrierScheduleNone applyCarrierScheduleMode = iota
	applyCarrierScheduleNoOp
	applyCarrierScheduleVerifyPending
	applyCarrierScheduleHostRoute
	applyCarrierScheduleDirectProjection
)

type applyOrderScheduleFact struct {
	ref              string
	classID          string
	requiresMutation bool
}

type applyDelegateScheduleFact struct {
	ref    string
	action reconcile.DelegateAction
	work   operationplan.DelegateWork
}

func compileApplySchedule(
	input applyScheduleInput,
	legacy operationplan.Demand,
) (applyForwardEffectSchedule, error) {
	if err := validateApplyScheduleReferences(input); err != nil {
		return applyForwardEffectSchedule{}, err
	}
	var builder operationplan.EffectStructureBuilder
	statefile := applyStatefileSchedule{builder: &builder}
	providerNode := compileApplyProviderSchedule(&builder, &statefile, input.providerRoutes)
	continuation, err := compileApplyContinuationPlan(&builder, &statefile, input)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	finalNode := compileApplyFinalSchedule(&builder, input, continuation.segment)
	if statefile.err != nil {
		return applyForwardEffectSchedule{}, statefile.err
	}
	finalPhase := builder.ForwardPhase("apply/final", finalNode)
	finalStructure, err := builder.Compile(finalPhase)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	fullNodes := make([]operationplan.EffectNode, 0, 5)
	if len(input.providerRoutes) != 0 {
		fullNodes = append(
			fullNodes,
			builder.Step("apply/provider/pre-barrier", operationplan.EffectStepValidateBarrier),
			builder.ForwardPhase("apply/provider", providerNode),
			builder.Step("apply/provider/post-barrier", operationplan.EffectStepValidateBarrier),
			builder.Step("apply/provider/replan-barrier", operationplan.EffectStepValidateBarrier),
		)
	}
	fullNodes = append(fullNodes, finalPhase)
	fullStructure, err := builder.Compile(operationplan.EffectSequence(fullNodes...))
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	if err := requireLegacyApplyDemandDominance(fullStructure, legacy); err != nil {
		return applyForwardEffectSchedule{}, err
	}
	return applyForwardEffectSchedule{
		full:         fullStructure,
		final:        finalStructure,
		continuation: continuation,
	}, nil
}

func validateApplyScheduleReferences(input applyScheduleInput) error {
	seen := make(map[string]string)
	validate := func(owner string, ref string) error {
		if strings.TrimSpace(ref) == "" || strings.TrimSpace(ref) != ref {
			return fmt.Errorf("apply schedule %s reference %q is not canonical", owner, ref)
		}
		if previous, exists := seen[ref]; exists {
			return fmt.Errorf(
				"apply schedule reference %q is shared by %s and %s",
				ref,
				previous,
				owner,
			)
		}
		seen[ref] = owner
		return nil
	}
	for index, route := range input.providerRoutes {
		if err := validate(fmt.Sprintf("provider route %d", index), route.ref); err != nil {
			return err
		}
	}
	for index, removal := range input.carrierRemovals {
		if err := validate(fmt.Sprintf("carrier removal %d", index), removal.ref); err != nil {
			return err
		}
	}
	for index, route := range input.finalRoutes {
		if err := validate(fmt.Sprintf("final route %d", index), route.ref); err != nil {
			return err
		}
	}
	for index, class := range input.orderClasses {
		if err := validate(fmt.Sprintf("relation-order class %d", index), class.ref); err != nil {
			return err
		}
	}
	for index, delegate := range input.delegates {
		if err := validate(fmt.Sprintf("delegate %d", index), delegate.ref); err != nil {
			return err
		}
	}
	return nil
}

func compileApplyContinuationPlan(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	input applyScheduleInput,
) (applyContinuationPlan, error) {
	initiallyBound := statefile.bound
	segment, carrierRemovalSegment := compileApplyContinuationSchedule(
		builder,
		statefile,
		input,
	)
	carrierRemovalStructure, err := builder.Compile(builder.ForwardPhase(
		"apply/carrier-removals",
		carrierRemovalSegment,
	))
	if err != nil {
		return applyContinuationPlan{}, err
	}
	structure, err := builder.Compile(builder.ForwardPhase(
		"apply/continuation",
		segment,
	))
	if err != nil {
		return applyContinuationPlan{}, err
	}
	return applyContinuationPlan{
		segment:                 segment,
		structure:               structure,
		phaseEstablished:        input.coreChanged,
		carrierRemovalStructure: carrierRemovalStructure,
		carrierPhaseEstablished: input.coreChanged || input.hasGlobalRetirement,
		statefileInitiallyBound: initiallyBound,
		carrierRemovals:         append([]applyCarrierScheduleFact(nil), input.carrierRemovals...),
		finalRoutes:             append([]applyRouteScheduleFact(nil), input.finalRoutes...),
		orderClasses:            append([]applyOrderScheduleFact(nil), input.orderClasses...),
		mayReclassifyOrder:      input.mayReclassifyOrder,
		delegates:               append([]applyDelegateScheduleFact(nil), input.delegates...),
		available:               true,
	}, nil
}

func compileApplyProviderSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	routes []applyRouteScheduleFact,
) operationplan.EffectNode {
	nodes := make([]operationplan.EffectNode, 0, len(routes))
	for _, route := range routes {
		nodes = append(nodes, compileApplyRouteSchedule(builder, statefile, route, true))
	}
	return operationplan.EffectSequence(nodes...)
}

func compileApplyFinalSchedule(
	builder *operationplan.EffectStructureBuilder,
	input applyScheduleInput,
	continuation operationplan.EffectNode,
) operationplan.EffectNode {
	nodes := make([]operationplan.EffectNode, 0, 3)
	nodes = append(nodes, builder.Step(
		"apply/effect-segment",
		operationplan.EffectStepNoOp,
	))
	if input.coreChanged {
		nodes = append(nodes, input.core)
	} else {
		nodes = append(nodes, builder.Step(
			"apply/effect-segment/no-change",
			operationplan.EffectStepNoOp,
		))
	}
	nodes = append(nodes, continuation)
	return operationplan.EffectSequence(nodes...)
}

func compileApplyContinuationSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	input applyScheduleInput,
) (operationplan.EffectNode, operationplan.EffectNode) {
	nodes := make([]operationplan.EffectNode, 0, 7)
	if input.hasGlobalRetirement {
		nodes = append(nodes, compileApplyGlobalCarrierBatchSettlementSchedule(
			builder,
			"apply/global-claim-retirements",
		))
	}
	carrierRemovals := compileApplyCarrierRemovalSchedules(
		builder,
		statefile,
		input.carrierRemovals,
	)
	nodes = append(nodes, carrierRemovals)
	nodes = append(nodes, compileApplyFinalRouteSchedule(
		builder,
		statefile,
		input.finalRoutes,
	)...)
	if len(input.orderClasses) != 0 {
		nodes = append(
			nodes,
			builder.Step(
				"apply/relation-order/reobserve",
				operationplan.EffectStepObservation,
			),
			compileApplyFailFastChoice(builder, "apply/relation-order/admission"),
		)
	}
	for _, class := range input.orderClasses {
		nodes = append(nodes, compileApplyOrderSchedule(
			builder,
			class,
			input.mayReclassifyOrder,
		))
	}
	if len(input.delegates) != 0 {
		nodes = append(nodes, compileApplyDelegateSchedule(
			builder,
			statefile,
			input.delegates,
		))
	}
	if input.hasGlobalAdoption {
		nodes = append(nodes, compileApplyGlobalCarrierBatchSettlementSchedule(
			builder,
			"apply/global-claim-adoptions",
		))
	}
	return operationplan.EffectSequence(nodes...), carrierRemovals
}

func compileApplyCarrierRemovalSchedules(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	removals []applyCarrierScheduleFact,
) operationplan.EffectNode {
	nodes := make([]operationplan.EffectNode, 0, len(removals))
	for _, removal := range removals {
		if removal.mode == applyCarrierScheduleNone {
			continue
		}
		nodes = append(nodes, compileApplyCarrierRemovalSchedule(
			builder,
			statefile,
			removal,
		))
	}
	return operationplan.EffectSequence(nodes...)
}

func compileApplyGlobalCarrierBatchSettlementSchedule(
	builder *operationplan.EffectStructureBuilder,
	ref string,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		compileApplyCheckedStep(
			builder,
			ref+"/pre-registry",
			operationplan.EffectStepForwardEffect,
		),
		compileApplyCheckedStep(
			builder,
			ref+"/persistence",
			operationplan.EffectStepPersistence,
		),
		compileApplyCheckedStep(
			builder,
			ref+"/post-registry",
			operationplan.EffectStepForwardEffect,
		),
	)
}

func compileApplyFinalRouteSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	routes []applyRouteScheduleFact,
) []operationplan.EffectNode {
	preflightRejected := false
	preflightNodes := make([]operationplan.EffectNode, 0, len(routes))
	promotions := make([]applyRouteScheduleFact, 0, len(routes))
	prepared := make([]applyRouteScheduleFact, 0, len(routes))
	for _, route := range routes {
		if route.work.InvokesHost {
			preflightNodes = append(preflightNodes, operationplan.EffectSequence(
				builder.Step(route.ref+"/preflight", operationplan.EffectStepObservation),
				builder.Choice(
					route.ref+"/preflight-outcome",
					builder.Step(route.ref+"/preflight-accepted", operationplan.EffectStepNoOp),
					builder.Step(route.ref+"/preflight-rejected", operationplan.EffectStepNoOp),
				),
			))
		}
		switch {
		case route.work.InvokesHost && route.preflightRejected:
			preflightRejected = true
		case route.work.InvokesHost:
			prepared = append(prepared, route)
		case route.work.Promotion:
			promotions = append(promotions, route)
		}
	}
	if !preflightRejected && len(promotions) == 0 && len(prepared) == 0 {
		return nil
	}

	nodes := make([]operationplan.EffectNode, 0, len(preflightNodes)+len(promotions)+len(prepared)+5)
	nodes = append(nodes, preflightNodes...)
	nodes = append(nodes, compileApplyCheckedStep(
		builder,
		"apply/final-routes/initial-project-root",
		operationplan.EffectStepObservation,
	))
	if preflightRejected || len(promotions) != 0 {
		nodes = append(
			nodes,
			compileApplyCheckedStep(
				builder,
				"apply/final-routes/preflight-state/forward",
				operationplan.EffectStepForwardEffect,
			),
			statefile.checkedEnsure("apply/final-routes/preflight-state/statefile"),
		)
	}
	if preflightRejected {
		nodes = append(
			nodes,
			statefile.checkedPublications("apply/final-routes/preflight-records", 1),
			statefile.checkedValidations("apply/final-routes/preflight-records", 1),
			compileApplyCheckedStep(
				builder,
				"apply/final-routes/preflight-project-root",
				operationplan.EffectStepObservation,
			),
		)
	}
	for _, route := range promotions {
		nodes = append(nodes, compileApplyRoutePromotionSchedule(builder, statefile, route))
	}
	for _, route := range prepared {
		nodes = append(nodes, compileApplyPreparedRouteSchedule(builder, statefile, route))
	}
	nodes = append(nodes, builder.Choice(
		"apply/final-routes/outcome",
		builder.Step("apply/final-routes/success", operationplan.EffectStepNoOp),
		builder.Step("apply/final-routes/failure", operationplan.EffectStepTerminal),
	))
	return nodes
}

func compileApplyRoutePromotionSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	route applyRouteScheduleFact,
) operationplan.EffectNode {
	return compileApplyGlobalCarrierPromotionSettlementSchedule(
		builder,
		route.ref,
		statefile.checkedValidations(route.ref+"/statefile/pre-registry", 1),
		statefile.checkedValidations(route.ref+"/statefile/post-registry", 1),
		statefile.checkedPublications(route.ref+"/statefile/project-claim", 1),
		statefile.checkedValidations(route.ref+"/statefile/post-claim", 1),
	)
}

func compileApplyGlobalCarrierPromotionSettlementSchedule(
	builder *operationplan.EffectStructureBuilder,
	ref string,
	preRegistryValidation operationplan.EffectNode,
	postRegistryValidation operationplan.EffectNode,
	claimPublication operationplan.EffectNode,
	postClaimValidation operationplan.EffectNode,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		compileApplyCheckedStep(
			builder,
			ref+"/declarations-before-registry",
			operationplan.EffectStepObservation,
		),
		compileApplyCheckedStep(
			builder,
			ref+"/project-root-before-registry",
			operationplan.EffectStepObservation,
		),
		preRegistryValidation,
		compileApplyCheckedStep(
			builder,
			ref+"/global-registry",
			operationplan.EffectStepPersistence,
		),
		postRegistryValidation,
		compileApplyCheckedStep(
			builder,
			ref+"/registry-visibility",
			operationplan.EffectStepObservation,
		),
		claimPublication,
		postClaimValidation,
		compileApplyCheckedStep(
			builder,
			ref+"/statefile-visibility",
			operationplan.EffectStepObservation,
		),
		compileApplyCheckedStep(
			builder,
			ref+"/project-root-after-claim",
			operationplan.EffectStepObservation,
		),
		compileApplyCheckedStep(
			builder,
			ref+"/declarations-after-claim",
			operationplan.EffectStepObservation,
		),
	)
}

func compileApplyPreparedRouteSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	route applyRouteScheduleFact,
) operationplan.EffectNode {
	bound := operationplan.EffectSequence(
		statefile.checkedPublications(route.ref+"/statefile/pending", 1),
		compileApplyCheckedStep(
			builder,
			route.ref+"/context-before-host",
			operationplan.EffectStepObservation,
		),
		statefile.checkedValidations(route.ref+"/statefile/pre-host", 1),
		builder.Step(route.ref+"/host", operationplan.EffectStepExternal),
		statefile.validations(route.ref+"/statefile/post-host", 1),
		builder.Choice(
			route.ref+"/post-host-outcome",
			builder.Step(route.ref+"/post-host-success", operationplan.EffectStepNoOp),
			operationplan.EffectSequence(
				builder.Step(
					route.ref+"/post-host-failure/classify",
					operationplan.EffectStepObservation,
				),
				builder.Step(route.ref+"/post-host-failure", operationplan.EffectStepTerminal),
			),
			builder.Step(route.ref+"/post-host-canceled", operationplan.EffectStepTerminal),
		),
		builder.Step(route.ref+"/post-host-observation", operationplan.EffectStepObservation),
		builder.Choice(
			route.ref+"/classification",
			compileApplyNormalRouteSettlement(builder, statefile, route),
			compileApplyFallbackRouteSettlement(builder, statefile, route),
			builder.Step(route.ref+"/classification-failure", operationplan.EffectStepTerminal),
		),
	)
	bindingFailed := operationplan.EffectSequence(
		builder.Step(route.ref+"/binding-failed/host", operationplan.EffectStepExternal),
		builder.Step(route.ref+"/binding-failed/classify", operationplan.EffectStepObservation),
		builder.Step(route.ref+"/binding-failed/declarations-current", operationplan.EffectStepObservation),
		builder.Step(route.ref+"/binding-failed", operationplan.EffectStepTerminal),
	)
	return operationplan.EffectSequence(
		compileApplyCheckedStep(
			builder,
			route.ref+"/forward",
			operationplan.EffectStepForwardEffect,
		),
		statefile.checkedEnsure(route.ref+"/statefile"),
		builder.Step(route.ref+"/binding", operationplan.EffectStepObservation),
		builder.Choice(route.ref+"/binding-outcome", bindingFailed, bound),
	)
}

func compileApplyNormalRouteSettlement(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	route applyRouteScheduleFact,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		compileApplyCheckedStep(
			builder,
			route.ref+"/project-root-before-settlement",
			operationplan.EffectStepObservation,
		),
		builder.Choice(
			route.ref+"/claim-promotion",
			builder.Step(route.ref+"/claim-promotion-none", operationplan.EffectStepNoOp),
			compileApplyObservedRouteClaimSchedule(builder, statefile, route),
		),
		statefile.checkedValidations(route.ref+"/statefile/pre-retirement", 1),
		statefile.checkedPublications(route.ref+"/statefile/retirement", 1),
		compileApplyCheckedStep(
			builder,
			route.ref+"/attempt-record",
			operationplan.EffectStepObservation,
		),
		statefile.checkedValidations(route.ref+"/statefile/pre-attempt", 1),
		statefile.checkedPublications(route.ref+"/statefile/attempt", 1),
		statefile.checkedValidations(route.ref+"/statefile/post-attempt", 1),
		compileApplyCheckedStep(
			builder,
			route.ref+"/project-root-after-settlement",
			operationplan.EffectStepObservation,
		),
		compileApplyCheckedStep(
			builder,
			route.ref+"/binding-release",
			operationplan.EffectStepCleanup,
		),
		compileApplyCheckedStep(
			builder,
			route.ref+"/declarations-current",
			operationplan.EffectStepObservation,
		),
	)
}

func compileApplyObservedRouteClaimSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	route applyRouteScheduleFact,
) operationplan.EffectNode {
	if route.work.Global {
		return operationplan.EffectSequence(
			statefile.checkedValidations(route.ref+"/statefile/pre-global-claim", 1),
			compileApplyGlobalCarrierPromotionSettlementSchedule(
				builder,
				route.ref+"/observed-global-claim",
				statefile.checkedValidations(route.ref+"/statefile/pre-global-registry", 1),
				statefile.checkedValidations(route.ref+"/statefile/post-global-registry", 1),
				statefile.checkedPublications(route.ref+"/statefile/global-claim", 1),
				statefile.checkedValidations(route.ref+"/statefile/post-global-claim", 1),
			),
		)
	}
	return operationplan.EffectSequence(
		statefile.checkedValidations(route.ref+"/statefile/pre-project-claim", 1),
		statefile.checkedPublications(route.ref+"/statefile/project-claim", 1),
	)
}

func compileApplyFallbackRouteSettlement(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	route applyRouteScheduleFact,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		compileApplyCheckedStep(
			builder,
			route.ref+"/fallback-classification",
			operationplan.EffectStepObservation,
		),
		compileApplyCheckedStep(
			builder,
			route.ref+"/fallback-project-root",
			operationplan.EffectStepObservation,
		),
		statefile.checkedValidations(route.ref+"/statefile/fallback-pre-retirement", 1),
		statefile.checkedPublications(route.ref+"/statefile/fallback-retirement", 1),
		compileApplyCheckedStep(
			builder,
			route.ref+"/fallback-attempt-record",
			operationplan.EffectStepObservation,
		),
		statefile.checkedValidations(route.ref+"/statefile/fallback-pre-attempt", 1),
		statefile.checkedPublications(route.ref+"/statefile/fallback-attempt", 1),
		statefile.checkedValidations(route.ref+"/statefile/fallback-post-attempt", 1),
		compileApplyCheckedStep(
			builder,
			route.ref+"/fallback-project-root-after-settlement",
			operationplan.EffectStepObservation,
		),
		compileApplyCheckedStep(
			builder,
			route.ref+"/fallback-binding-release",
			operationplan.EffectStepCleanup,
		),
		builder.Step(route.ref+"/fallback-failure", operationplan.EffectStepTerminal),
	)
}

func compileApplyRouteSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	route applyRouteScheduleFact,
	provider bool,
) operationplan.EffectNode {
	nodes := []operationplan.EffectNode{
		builder.Step(route.ref+"/forward", operationplan.EffectStepForwardEffect),
	}
	switch {
	case route.work.InvokesHost:
		validations := 7
		if route.work.Global {
			validations = 10
		}
		nodes = append(
			nodes,
			statefile.ensure(route.ref+"/statefile"),
			statefile.publications(route.ref+"/statefile/pending", 1),
			statefile.validations(route.ref+"/statefile/pre-host", 1),
			builder.Step(route.ref+"/host", operationplan.EffectStepExternal),
			statefile.validations(route.ref+"/statefile/post-host", validations-2),
			statefile.publications(route.ref+"/statefile/settlement", 3),
		)
		if provider {
			nodes = append(nodes, compileApplyFailFastChoice(builder, route.ref+"/outcome"))
		} else {
			failureRef := route.ref + "/ordinary-failure"
			nodes = append(nodes, builder.Choice(
				route.ref+"/outcome",
				builder.Step(route.ref+"/success", operationplan.EffectStepNoOp),
				builder.Trigger(
					failureRef,
					builder.Step(route.ref+"/ordinary", operationplan.EffectStepNoOp),
				),
				builder.Step(route.ref+"/terminal", operationplan.EffectStepTerminal),
			))
		}
	case route.work.Promotion:
		nodes = append(
			nodes,
			statefile.ensure(route.ref+"/statefile"),
			builder.Step(route.ref+"/promotion", operationplan.EffectStepPersistence),
			statefile.validations(route.ref+"/statefile/post-promotion", 3),
			statefile.publications(route.ref+"/statefile/promotion", 1),
			compileApplyFailFastChoice(builder, route.ref+"/outcome"),
		)
	case provider:
		nodes = append(nodes, builder.Step(route.ref+"/noop", operationplan.EffectStepNoOp))
	}
	return operationplan.EffectSequence(nodes...)
}

func compileApplyCarrierRemovalSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	removal applyCarrierScheduleFact,
) operationplan.EffectNode {
	switch removal.mode {
	case applyCarrierScheduleVerifyPending:
		return operationplan.EffectSequence(
			compileApplyCheckedStep(
				builder,
				removal.ref+"/verify-current",
				operationplan.EffectStepObservation,
			),
			compileApplyCheckedStep(
				builder,
				removal.ref+"/forward",
				operationplan.EffectStepForwardEffect,
			),
			statefile.checkedEnsure(removal.ref+"/statefile"),
			compileApplyCarrierRetirementSchedule(
				builder,
				statefile,
				removal,
				false,
			),
		)
	case applyCarrierScheduleDirectProjection:
		return operationplan.EffectSequence(
			compileApplyCheckedStep(
				builder,
				removal.ref+"/prepare-direct",
				operationplan.EffectStepObservation,
			),
			compileApplyCheckedStepWithFailureCleanup(
				builder,
				removal.ref+"/bind-direct",
				operationplan.EffectStepObservation,
			),
			compileApplyCheckedStepWithFailureCleanup(
				builder,
				removal.ref+"/forward",
				operationplan.EffectStepForwardEffect,
			),
			statefile.checkedEnsureWithFailureCleanup(removal.ref+"/statefile"),
			compileApplyCheckedStepWithFailureCleanup(
				builder,
				removal.ref+"/effect-baselines",
				operationplan.EffectStepObservation,
			),
			statefile.checkedPublicationsWithFailureCleanup(
				removal.ref+"/statefile/pending",
				1,
			),
			statefile.checkedValidationsWithFailureCleanup(
				removal.ref+"/statefile/pre-effect",
				1,
			),
			compileApplyCheckedStepWithFailureCleanup(
				builder,
				removal.ref+"/effect",
				operationplan.EffectStepPersistence,
			),
			statefile.checkedValidationsWithFailureCleanup(
				removal.ref+"/statefile/post-effect",
				1,
			),
			compileApplyCheckedStepWithFailureCleanup(
				builder,
				removal.ref+"/retained-boundary",
				operationplan.EffectStepObservation,
			),
			compileApplyCheckedStepWithFailureCleanup(
				builder,
				removal.ref+"/verify-current",
				operationplan.EffectStepObservation,
			),
			compileApplyCarrierRetirementSchedule(
				builder,
				statefile,
				removal,
				true,
			),
			compileApplyCheckedStep(
				builder,
				removal.ref+"/bound-close",
				operationplan.EffectStepCleanup,
			),
		)
	case applyCarrierScheduleHostRoute:
		return compileApplyCarrierHostRouteSchedule(builder, statefile, removal)
	case applyCarrierScheduleNoOp:
		return builder.Step(removal.ref+"/no-op", operationplan.EffectStepNoOp)
	case applyCarrierScheduleNone:
		return operationplan.EffectSequence()
	default:
		if statefile.err == nil {
			statefile.err = fmt.Errorf("apply carrier removal mode %d is invalid", removal.mode)
		}
		return operationplan.EffectSequence()
	}
}

func compileApplyCarrierHostRouteSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	removal applyCarrierScheduleFact,
) operationplan.EffectNode {
	preflightStatefile := statefile.branch()
	preflightRef := removal.ref + "/preflight-rejected"
	preflightRejected := operationplan.EffectSequence(
		compileApplyCheckedStep(
			builder,
			preflightRef+"/record",
			operationplan.EffectStepObservation,
		),
		compileApplyCheckedStep(
			builder,
			preflightRef+"/forward",
			operationplan.EffectStepForwardEffect,
		),
		preflightStatefile.checkedEnsure(preflightRef+"/statefile"),
		compileApplyCarrierAttemptPersistenceSchedule(
			builder,
			&preflightStatefile,
			preflightRef+"/attempt",
		),
		builder.Step(preflightRef+"/failure", operationplan.EffectStepTerminal),
	)

	preparedStatefile := statefile.branch()
	missingBindingStatefile := preparedStatefile.branch()
	missingBinding := compileApplyCarrierMissingBindingSchedule(
		builder,
		&missingBindingStatefile,
		removal,
	)
	boundStatefile := preparedStatefile.branch()
	prepared := compileApplyCarrierPreparedHostSchedule(builder, &boundStatefile, removal)
	preparedStatefile.adopt(boundStatefile)
	statefile.adopt(preparedStatefile)
	if statefile.err == nil {
		statefile.err = preflightStatefile.err
	}
	if statefile.err == nil {
		statefile.err = missingBindingStatefile.err
	}

	return operationplan.EffectSequence(
		builder.Step(removal.ref+"/preflight", operationplan.EffectStepObservation),
		builder.Choice(
			removal.ref+"/preflight-outcome",
			preflightRejected,
			operationplan.EffectSequence(
				builder.Step(removal.ref+"/binding", operationplan.EffectStepObservation),
				builder.Choice(
					removal.ref+"/binding-outcome",
					missingBinding,
					prepared,
				),
			),
		),
	)
}

func compileApplyCarrierMissingBindingSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	removal applyCarrierScheduleFact,
) operationplan.EffectNode {
	ref := removal.ref + "/binding-failed"
	return operationplan.EffectSequence(
		compileApplyCheckedStep(
			builder,
			ref+"/forward",
			operationplan.EffectStepForwardEffect,
		),
		statefile.checkedEnsure(ref+"/statefile"),
		builder.Step(ref+"/host", operationplan.EffectStepExternal),
		compileApplyCheckedStep(
			builder,
			ref+"/classify",
			operationplan.EffectStepObservation,
		),
		compileApplyCarrierAttemptPersistenceSchedule(
			builder,
			statefile,
			ref+"/attempt",
		),
		builder.Step(ref+"/failure", operationplan.EffectStepTerminal),
	)
}

func compileApplyCarrierPreparedHostSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	removal applyCarrierScheduleFact,
) operationplan.EffectNode {
	ref := removal.ref + "/prepared"
	return operationplan.EffectSequence(
		compileApplyCheckedStepWithFailureCleanup(
			builder,
			ref+"/baselines",
			operationplan.EffectStepObservation,
		),
		compileApplyCheckedStepWithFailureCleanup(
			builder,
			ref+"/forward",
			operationplan.EffectStepForwardEffect,
		),
		statefile.checkedEnsureWithFailureCleanup(ref+"/statefile"),
		statefile.checkedPublicationsWithFailureCleanup(
			ref+"/statefile/pending",
			1,
		),
		compileApplyCheckedStepWithFailureCleanup(
			builder,
			ref+"/context-before-host",
			operationplan.EffectStepObservation,
		),
		statefile.checkedValidationsWithFailureCleanup(
			ref+"/statefile/pre-host",
			1,
		),
		builder.Step(ref+"/host", operationplan.EffectStepExternal),
		statefile.validations(ref+"/statefile/post-host", 1),
		builder.Choice(
			ref+"/post-host-outcome",
			builder.Step(ref+"/post-host-success", operationplan.EffectStepNoOp),
			operationplan.EffectSequence(
				builder.Step(
					ref+"/post-host-failure/classify",
					operationplan.EffectStepObservation,
				),
				builder.Step(ref+"/post-host-failure", operationplan.EffectStepTerminal),
			),
			builder.Step(ref+"/post-host-canceled", operationplan.EffectStepTerminal),
		),
		builder.Step(
			ref+"/post-host-observation",
			operationplan.EffectStepObservation,
		),
		builder.Step(ref+"/classify", operationplan.EffectStepObservation),
		builder.Choice(
			ref+"/classify-outcome",
			builder.Step(ref+"/classify-usable", operationplan.EffectStepNoOp),
			builder.Step(ref+"/classify-failure", operationplan.EffectStepTerminal),
		),
		builder.Step(ref+"/retained-boundary", operationplan.EffectStepObservation),
		compileApplyCheckedStep(
			builder,
			ref+"/attempt-record",
			operationplan.EffectStepObservation,
		),
		compileApplyCarrierAttemptPersistenceSchedule(
			builder,
			statefile,
			ref+"/attempt",
		),
		builder.Choice(
			ref+"/attempt-outcome",
			compileApplyCarrierRetirementSchedule(
				builder,
				statefile,
				removal,
				false,
			),
			builder.Step(
				ref+"/attempt-failure",
				operationplan.EffectStepTerminal,
			),
		),
	)
}

func compileApplyCarrierAttemptPersistenceSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	ref string,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		statefile.checkedValidations(ref+"/pre-persistence", 1),
		statefile.checkedPublications(ref+"/persistence", 1),
		statefile.checkedValidations(ref+"/post-persistence", 1),
	)
}

func compileApplyCarrierRetirementSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	removal applyCarrierScheduleFact,
	cleanupOnFailure bool,
) operationplan.EffectNode {
	checkedStep := compileApplyCheckedStep
	if cleanupOnFailure {
		checkedStep = compileApplyCheckedStepWithFailureCleanup
	}
	checkedValidations := statefile.checkedValidations
	checkedPublications := statefile.checkedPublications
	if cleanupOnFailure {
		checkedValidations = statefile.checkedValidationsWithFailureCleanup
		checkedPublications = statefile.checkedPublicationsWithFailureCleanup
	}
	nodes := []operationplan.EffectNode{
		checkedValidations(removal.ref+"/statefile/pre-retirement", 1),
		checkedPublications(removal.ref+"/statefile/retirement", 1),
		checkedValidations(removal.ref+"/statefile/post-retirement", 1),
	}
	if removal.scope == target.ScopeGlobal {
		nodes = append(
			[]operationplan.EffectNode{
				checkedStep(
					builder,
					removal.ref+"/derive-global-retirement",
					operationplan.EffectStepObservation,
				),
			},
			nodes...,
		)
		nodes = append(
			nodes,
			checkedStep(
				builder,
				removal.ref+"/global-registry-retirement",
				operationplan.EffectStepPersistence,
			),
			checkedValidations(removal.ref+"/statefile/post-registry", 1),
		)
	}
	return operationplan.EffectSequence(nodes...)
}

func compileApplyOrderSchedule(
	builder *operationplan.EffectStructureBuilder,
	class applyOrderScheduleFact,
	mayReclassify bool,
) operationplan.EffectNode {
	mutating := operationplan.EffectSequence(
		compileApplyCheckedStep(
			builder,
			class.ref+"/forward",
			operationplan.EffectStepForwardEffect,
		),
		compileApplyCheckedStep(
			builder,
			class.ref+"/binding",
			operationplan.EffectStepObservation,
		),
		builder.Step(class.ref+"/external", operationplan.EffectStepExternal),
		compileApplyCheckedStep(
			builder,
			class.ref+"/settlement",
			operationplan.EffectStepObservation,
		),
	)
	switch {
	case mayReclassify:
		return builder.Choice(
			class.ref+"/choice",
			builder.Step(class.ref+"/noop", operationplan.EffectStepNoOp),
			mutating,
		)
	case class.requiresMutation:
		return mutating
	default:
		return builder.Step(class.ref+"/noop", operationplan.EffectStepNoOp)
	}
}

func compileApplyDelegateSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	delegates []applyDelegateScheduleFact,
) operationplan.EffectNode {
	persist := false
	for _, action := range delegates {
		if action.work.SchedulesAttempt || action.work.Blocked {
			persist = true
			break
		}
	}
	nodes := make([]operationplan.EffectNode, 0, len(delegates)*6+9)
	nodes = append(nodes, builder.Step(
		"apply/delegates/admission",
		operationplan.EffectStepObservation,
	))
	if persist {
		nodes = append(
			nodes,
			compileApplyOptionalStep(
				builder,
				"apply/delegates/forward",
				operationplan.EffectStepForwardEffect,
			),
			statefile.optionalEnsure("apply/delegates/statefile"),
		)
	}
	for _, action := range delegates {
		nodes = append(nodes, compileApplyOptionalStep(
			builder,
			action.ref+"/declarations-before",
			operationplan.EffectStepObservation,
		))
		if persist {
			nodes = append(nodes, compileApplyOptionalStep(
				builder,
				action.ref+"/statefile/pre-attempt/validate",
				operationplan.EffectStepValidateDescendant,
			))
		} else {
			nodes = append(nodes, compileApplyOptionalStep(
				builder,
				action.ref+"/pre-state-dir",
				operationplan.EffectStepValidateStateDir,
			))
		}
		nodes = append(nodes, compileApplyOptionalStep(
			builder,
			action.ref+"/attempt",
			applyDelegateAttemptKind(action.work),
		))
		if persist {
			nodes = append(nodes, compileApplyOptionalStep(
				builder,
				action.ref+"/statefile/post-attempt/validate",
				operationplan.EffectStepValidateDescendant,
			))
		} else {
			nodes = append(nodes, compileApplyOptionalStep(
				builder,
				action.ref+"/post-state-dir",
				operationplan.EffectStepValidateStateDir,
			))
		}
		nodes = append(
			nodes,
			compileApplyOptionalStep(
				builder,
				action.ref+"/declarations-after",
				operationplan.EffectStepObservation,
			),
			builder.Choice(
				action.ref+"/outcome",
				builder.Step(action.ref+"/success", operationplan.EffectStepNoOp),
				builder.Step(action.ref+"/ordinary", operationplan.EffectStepNoOp),
				builder.Step(action.ref+"/skipped", operationplan.EffectStepNoOp),
			),
		)
	}
	nodes = append(nodes, builder.Step(
		"apply/delegates/result",
		operationplan.EffectStepObservation,
	))
	if persist {
		nodes = append(
			nodes,
			compileApplyOptionalStep(
				builder,
				"apply/delegates/statefile/pre-persistence/validate",
				operationplan.EffectStepValidateDescendant,
			),
			compileApplyOptionalStep(
				builder,
				"apply/delegates/statefile/persistence/publish",
				operationplan.EffectStepPublishDescendant,
			),
			compileApplyOptionalStep(
				builder,
				"apply/delegates/statefile/post-persistence/validate",
				operationplan.EffectStepValidateDescendant,
			),
			compileApplyOptionalStep(
				builder,
				"apply/delegates/project-root",
				operationplan.EffectStepObservation,
			),
		)
	}
	nodes = append(nodes, builder.Choice(
		"apply/delegates/outcome",
		builder.Step("apply/delegates/outcome/success", operationplan.EffectStepNoOp),
		builder.Step("apply/delegates/outcome/failure", operationplan.EffectStepTerminal),
	))
	return operationplan.EffectSequence(nodes...)
}

func compileApplyOptionalStep(
	builder *operationplan.EffectStructureBuilder,
	ref string,
	kind operationplan.EffectStepKind,
) operationplan.EffectNode {
	return builder.Choice(
		ref+"/execution",
		builder.Step(ref, kind),
		builder.Step(ref+"/skipped", operationplan.EffectStepNoOp),
	)
}

func applyDelegateAttemptKind(work operationplan.DelegateWork) operationplan.EffectStepKind {
	if work.SchedulesAttempt {
		return operationplan.EffectStepExternal
	}
	return operationplan.EffectStepNoOp
}

func compileApplyFailFastPersistence(
	builder *operationplan.EffectStructureBuilder,
	ref string,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Step(ref+"/persistence", operationplan.EffectStepPersistence),
		compileApplyFailFastChoice(builder, ref+"/outcome"),
	)
}

func compileApplyCheckedStep(
	builder *operationplan.EffectStructureBuilder,
	ref string,
	kind operationplan.EffectStepKind,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Step(ref, kind),
		compileApplyFailFastChoice(builder, ref+"/outcome"),
	)
}

func compileApplyCheckedStepWithFailureCleanup(
	builder *operationplan.EffectStructureBuilder,
	ref string,
	kind operationplan.EffectStepKind,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Step(ref, kind),
		compileApplyFailFastChoiceWithFailureCleanup(builder, ref+"/outcome"),
	)
}

func compileApplyFailFastChoice(
	builder *operationplan.EffectStructureBuilder,
	ref string,
) operationplan.EffectNode {
	return builder.Choice(
		ref,
		builder.Step(ref+"/success", operationplan.EffectStepNoOp),
		builder.Step(ref+"/failure", operationplan.EffectStepTerminal),
	)
}

func compileApplyFailFastChoiceWithFailureCleanup(
	builder *operationplan.EffectStructureBuilder,
	ref string,
) operationplan.EffectNode {
	return builder.Choice(
		ref,
		builder.Step(ref+"/success", operationplan.EffectStepNoOp),
		operationplan.EffectSequence(
			builder.Step(ref+"/failure-cleanup", operationplan.EffectStepCleanup),
			builder.Step(ref+"/failure", operationplan.EffectStepTerminal),
		),
	)
}

type applyStatefileSchedule struct {
	builder *operationplan.EffectStructureBuilder
	bound   bool
	err     error
}

func (schedule *applyStatefileSchedule) checkedEnsure(prefix string) operationplan.EffectNode {
	return schedule.checkedEnsureWithCleanup(prefix, false)
}

func (schedule *applyStatefileSchedule) checkedEnsureWithFailureCleanup(
	prefix string,
) operationplan.EffectNode {
	return schedule.checkedEnsureWithCleanup(prefix, true)
}

func (schedule *applyStatefileSchedule) checkedEnsureWithCleanup(
	prefix string,
	cleanupOnFailure bool,
) operationplan.EffectNode {
	if schedule == nil || schedule.builder == nil {
		return operationplan.EffectSequence()
	}
	var authority operationplan.EffectNode
	if !schedule.bound {
		schedule.bound = true
		authority = schedule.builder.Choice(
			prefix+"/initial-authority",
			schedule.builder.Step(
				prefix+"/bind",
				operationplan.EffectStepBindDescendant,
			),
			schedule.builder.Step(
				prefix+"/validate-existing",
				operationplan.EffectStepValidateDescendant,
			),
		)
	} else {
		authority = schedule.builder.Step(
			prefix+"/ensure-validate",
			operationplan.EffectStepValidateDescendant,
		)
	}
	outcome := compileApplyFailFastChoice(schedule.builder, prefix+"/ensure-outcome")
	if cleanupOnFailure {
		outcome = compileApplyFailFastChoiceWithFailureCleanup(
			schedule.builder,
			prefix+"/ensure-outcome",
		)
	}
	return operationplan.EffectSequence(authority, outcome)
}

func (schedule *applyStatefileSchedule) optionalEnsure(prefix string) operationplan.EffectNode {
	if schedule == nil || schedule.builder == nil {
		return operationplan.EffectSequence()
	}
	var authority operationplan.EffectNode
	if !schedule.bound {
		schedule.bound = true
		authority = schedule.builder.Choice(
			prefix+"/initial-authority",
			schedule.builder.Step(
				prefix+"/bind",
				operationplan.EffectStepBindDescendant,
			),
			schedule.builder.Step(
				prefix+"/validate-existing",
				operationplan.EffectStepValidateDescendant,
			),
		)
	} else {
		authority = schedule.builder.Step(
			prefix+"/ensure-validate",
			operationplan.EffectStepValidateDescendant,
		)
	}
	return schedule.builder.Choice(
		prefix+"/execution",
		authority,
		schedule.builder.Step(prefix+"/skipped", operationplan.EffectStepNoOp),
	)
}

func (schedule *applyStatefileSchedule) branch() applyStatefileSchedule {
	if schedule == nil {
		return applyStatefileSchedule{}
	}
	return *schedule
}

func (schedule *applyStatefileSchedule) adopt(branch applyStatefileSchedule) {
	if schedule == nil {
		return
	}
	schedule.bound = branch.bound
	if schedule.err == nil {
		schedule.err = branch.err
	}
}

func (schedule *applyStatefileSchedule) checkedValidations(
	prefix string,
	count int,
) operationplan.EffectNode {
	return schedule.checkedValidationsWithCleanup(prefix, count, false)
}

func (schedule *applyStatefileSchedule) checkedValidationsWithFailureCleanup(
	prefix string,
	count int,
) operationplan.EffectNode {
	return schedule.checkedValidationsWithCleanup(prefix, count, true)
}

func (schedule *applyStatefileSchedule) checkedValidationsWithCleanup(
	prefix string,
	count int,
	cleanupOnFailure bool,
) operationplan.EffectNode {
	if schedule == nil || schedule.builder == nil {
		return operationplan.EffectSequence()
	}
	if count < 0 {
		if schedule.err == nil {
			schedule.err = fmt.Errorf("apply statefile validation count must not be negative")
		}
		return operationplan.EffectSequence()
	}
	step := compileApplyCheckedStep(
		schedule.builder,
		prefix+"/validate",
		operationplan.EffectStepValidateDescendant,
	)
	if cleanupOnFailure {
		step = compileApplyCheckedStepWithFailureCleanup(
			schedule.builder,
			prefix+"/validate",
			operationplan.EffectStepValidateDescendant,
		)
	}
	return schedule.builder.Repeat(count, step)
}

func (schedule *applyStatefileSchedule) checkedPublications(
	prefix string,
	count int,
) operationplan.EffectNode {
	return schedule.checkedPublicationsWithCleanup(prefix, count, false)
}

func (schedule *applyStatefileSchedule) checkedPublicationsWithFailureCleanup(
	prefix string,
	count int,
) operationplan.EffectNode {
	return schedule.checkedPublicationsWithCleanup(prefix, count, true)
}

func (schedule *applyStatefileSchedule) checkedPublicationsWithCleanup(
	prefix string,
	count int,
	cleanupOnFailure bool,
) operationplan.EffectNode {
	if schedule == nil || schedule.builder == nil {
		return operationplan.EffectSequence()
	}
	if count < 0 {
		if schedule.err == nil {
			schedule.err = fmt.Errorf("apply statefile publication count must not be negative")
		}
		return operationplan.EffectSequence()
	}
	step := compileApplyCheckedStep(
		schedule.builder,
		prefix+"/publish",
		operationplan.EffectStepPublishDescendant,
	)
	if cleanupOnFailure {
		step = compileApplyCheckedStepWithFailureCleanup(
			schedule.builder,
			prefix+"/publish",
			operationplan.EffectStepPublishDescendant,
		)
	}
	return schedule.builder.Repeat(count, step)
}

func (schedule *applyStatefileSchedule) ensure(prefix string) operationplan.EffectNode {
	if schedule == nil || schedule.builder == nil {
		return operationplan.EffectSequence()
	}
	if !schedule.bound {
		schedule.bound = true
		return schedule.builder.Choice(
			prefix+"/initial-authority",
			schedule.builder.Step(
				prefix+"/bind",
				operationplan.EffectStepBindDescendant,
			),
			schedule.builder.Step(
				prefix+"/validate-existing",
				operationplan.EffectStepValidateDescendant,
			),
		)
	}
	return schedule.builder.Step(
		prefix+"/ensure-validate",
		operationplan.EffectStepValidateDescendant,
	)
}

func (schedule *applyStatefileSchedule) validations(
	prefix string,
	count int,
) operationplan.EffectNode {
	if schedule == nil || schedule.builder == nil {
		return operationplan.EffectSequence()
	}
	if count < 0 {
		if schedule.err == nil {
			schedule.err = fmt.Errorf("apply statefile validation count must not be negative")
		}
		return operationplan.EffectSequence()
	}
	return schedule.builder.Repeat(
		count,
		schedule.builder.Step(prefix+"/validate", operationplan.EffectStepValidateDescendant),
	)
}

func (schedule *applyStatefileSchedule) publications(
	prefix string,
	count int,
) operationplan.EffectNode {
	if schedule == nil || schedule.builder == nil {
		return operationplan.EffectSequence()
	}
	if count < 0 {
		if schedule.err == nil {
			schedule.err = fmt.Errorf("apply statefile publication count must not be negative")
		}
		return operationplan.EffectSequence()
	}
	return schedule.builder.Repeat(
		count,
		schedule.builder.Step(prefix+"/publish", operationplan.EffectStepPublishDescendant),
	)
}

func applyCarrierScheduleFacts(
	actions []carrierabsence.Action,
) ([]applyCarrierScheduleFact, error) {
	works := carrierWorks(actions)
	result := make([]applyCarrierScheduleFact, 0, len(actions))
	for index := range actions {
		mode := applyCarrierScheduleNone
		switch {
		case actions[index].StateOnly():
			mode = applyCarrierScheduleNoOp
		case actions[index].VerifiesPendingRemoval():
			mode = applyCarrierScheduleVerifyPending
		case actions[index].InvokesHostRoute():
			mode = applyCarrierScheduleHostRoute
		case actions[index].MutatesDirectProjection():
			mode = applyCarrierScheduleDirectProjection
		}
		fingerprint, err := carrierRemovalScheduleFingerprint(actions[index])
		if err != nil {
			return nil, err
		}
		result = append(result, applyCarrierScheduleFact{
			ref:         applyOrdinalScheduleReference("apply/carrier-removal", index),
			action:      actions[index],
			fingerprint: fingerprint,
			work:        works[index],
			mode:        mode,
			scope:       actions[index].Scope(),
		})
	}
	return result, nil
}

func requireLegacyApplyDemandDominance(
	structure operationplan.EffectStructure,
	legacy operationplan.Demand,
) error {
	alternatives, err := structure.DemandAlternatives()
	if err != nil {
		return err
	}
	maximumBindings := 0
	maximumValidations := 0
	maximumCommits := 0
	for _, structural := range alternatives {
		maximumBindings = max(maximumBindings, structural.DescendantBindings())
		maximumValidations = max(maximumValidations, structural.DescendantValidations())
		maximumCommits = max(maximumCommits, structural.DescendantFileCommits())
		if legacy.EnsureCalls() < structural.EnsureCalls() ||
			legacy.BarrierValidationCalls() < structural.BarrierValidationCalls() ||
			legacy.StateDirValidationCalls() < structural.StateDirValidationCalls() ||
			legacy.DescendantBindings() < structural.DescendantBindings() ||
			legacy.DescendantValidations() < structural.DescendantValidations() ||
			legacy.DescendantFileCommits() < structural.DescendantFileCommits() {
			return fmt.Errorf(
				"legacy apply reservation does not dominate structural demand: structural=%d/%d/%d/%d/%d/%d legacy=%d/%d/%d/%d/%d/%d",
				structural.EnsureCalls(),
				structural.BarrierValidationCalls(),
				structural.StateDirValidationCalls(),
				structural.DescendantBindings(),
				structural.DescendantValidations(),
				structural.DescendantFileCommits(),
				legacy.EnsureCalls(),
				legacy.BarrierValidationCalls(),
				legacy.StateDirValidationCalls(),
				legacy.DescendantBindings(),
				legacy.DescendantValidations(),
				legacy.DescendantFileCommits(),
			)
		}
	}
	structuralDescendant := maximumBindings != 0 || maximumValidations != 0 || maximumCommits != 0
	legacyDescendant := legacy.DescendantBindings() != 0 ||
		legacy.DescendantValidations() != 0 ||
		legacy.DescendantFileCommits() != 0
	if structuralDescendant != legacyDescendant ||
		structuralDescendant && (maximumBindings != 1 || legacy.DescendantBindings() != 1 || legacy.DescendantPath() == "") {
		return fmt.Errorf(
			"apply descendant reservation is incompatible with structural demand: structural binding=%d validation=%d commit=%d legacy path=%q binding=%d validation=%d commit=%d",
			maximumBindings,
			maximumValidations,
			maximumCommits,
			legacy.DescendantPath(),
			legacy.DescendantBindings(),
			legacy.DescendantValidations(),
			legacy.DescendantFileCommits(),
		)
	}
	return nil
}
