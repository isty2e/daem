package apply

import (
	"fmt"
	"math"

	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
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
	ref  string
	work operationplan.RouteWork
}

type applyCarrierScheduleFact struct {
	ref  string
	work operationplan.CarrierWork
}

type applyOrderScheduleFact struct {
	ref              string
	requiresMutation bool
}

type applyDelegateScheduleFact struct {
	ref  string
	work operationplan.DelegateWork
}

func compileApplySchedule(
	input applyScheduleInput,
	legacy operationplan.Demand,
) (applyForwardEffectSchedule, error) {
	var builder operationplan.EffectStructureBuilder
	statefile := applyStatefileSchedule{builder: &builder}
	providerNode := compileApplyProviderSchedule(&builder, &statefile, input.providerRoutes)
	finalNode := compileApplyFinalSchedule(&builder, &statefile, input)
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
	structural, err := fullStructure.LegacyDemand()
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	if !sameApplyDemand(structural, legacy) {
		return applyForwardEffectSchedule{}, fmt.Errorf(
			"apply effect schedule demand differs from the legacy reservation: structural=%d/%d/%d/%d/%d/%d legacy=%d/%d/%d/%d/%d/%d",
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
	return applyForwardEffectSchedule{full: fullStructure, final: finalStructure}, nil
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
	statefile *applyStatefileSchedule,
	input applyScheduleInput,
) operationplan.EffectNode {
	nodes := make([]operationplan.EffectNode, 0, 8)
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
	if input.hasGlobalRetirement {
		nodes = append(nodes, compileApplyFailFastPersistence(
			builder,
			"apply/global-claim-retirements",
		))
	}
	for _, removal := range input.carrierRemovals {
		if !removal.work.InvokesHost &&
			!removal.work.MutatesDirect &&
			!removal.work.VerifiesPending {
			continue
		}
		nodes = append(nodes, compileApplyCarrierRemovalSchedule(
			builder,
			statefile,
			removal,
		))
	}
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
		nodes = append(nodes, compileApplyFailFastPersistence(
			builder,
			"apply/global-claim-adoptions",
		))
	}
	return operationplan.EffectSequence(nodes...)
}

func compileApplyFinalRouteSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	routes []applyRouteScheduleFact,
) []operationplan.EffectNode {
	promotions := make([]operationplan.EffectNode, 0)
	hosts := make([]operationplan.EffectNode, 0)
	ordinaryFailureRefs := make([]string, 0)
	for _, route := range routes {
		switch {
		case route.work.InvokesHost:
			failureRef := route.ref + "/ordinary-failure"
			hosts = append(hosts, compileApplyRouteSchedule(
				builder,
				statefile,
				route,
				false,
			))
			ordinaryFailureRefs = append(ordinaryFailureRefs, failureRef)
		case route.work.Promotion:
			if len(promotions) == 0 {
				promotions = append(promotions, builder.Step(
					"apply/final/promotions/forward",
					operationplan.EffectStepForwardEffect,
				))
			}
			promotions = append(
				promotions,
				statefile.ensure(route.ref+"/statefile"),
				builder.Step(route.ref+"/promotion", operationplan.EffectStepPersistence),
				statefile.validations(route.ref+"/statefile/post-promotion", 3),
				statefile.publications(route.ref+"/statefile/promotion", 1),
				compileApplyFailFastChoice(builder, route.ref+"/outcome"),
			)
		}
	}
	nodes := make([]operationplan.EffectNode, 0, len(hosts)+len(ordinaryFailureRefs)+1)
	if len(promotions) != 0 {
		nodes = append(nodes, operationplan.EffectSequence(promotions...))
	}
	nodes = append(nodes, hosts...)
	for _, failureRef := range ordinaryFailureRefs {
		nodes = append(nodes, builder.Conditional(
			failureRef,
			builder.Step(failureRef+"/terminal", operationplan.EffectStepTerminal),
		))
	}
	return nodes
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
	kind := operationplan.EffectStepObservation
	if removal.work.InvokesHost {
		kind = operationplan.EffectStepExternal
	} else if removal.work.MutatesDirect {
		kind = operationplan.EffectStepPersistence
	}
	return operationplan.EffectSequence(
		builder.Step(removal.ref+"/forward", operationplan.EffectStepForwardEffect),
		statefile.ensure(removal.ref+"/statefile"),
		statefile.publications(removal.ref+"/statefile/pending", 1),
		statefile.validations(removal.ref+"/statefile/pre-effect", 1),
		builder.Step(removal.ref+"/effect", kind),
		statefile.validations(removal.ref+"/statefile/post-effect", 6),
		statefile.publications(removal.ref+"/statefile/settlement", 2),
		compileApplyFailFastChoice(builder, removal.ref+"/outcome"),
	)
}

func compileApplyOrderSchedule(
	builder *operationplan.EffectStructureBuilder,
	class applyOrderScheduleFact,
	mayReclassify bool,
) operationplan.EffectNode {
	mutating := operationplan.EffectSequence(
		builder.Step(class.ref+"/forward", operationplan.EffectStepForwardEffect),
		builder.Step(class.ref+"/external", operationplan.EffectStepExternal),
		builder.Step(class.ref+"/observation", operationplan.EffectStepObservation),
		compileApplyFailFastChoice(builder, class.ref+"/outcome"),
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
	ordinaryFailures := make([]string, 0, len(delegates))
	nodes := make([]operationplan.EffectNode, 0, len(delegates)+2)
	if persist {
		nodes = append(
			nodes,
			builder.Step(
				"apply/delegates/forward",
				operationplan.EffectStepForwardEffect,
			),
			statefile.ensure("apply/delegates/statefile"),
		)
	}
	for _, action := range delegates {
		if persist {
			nodes = append(
				nodes,
				statefile.validations(action.ref+"/statefile/pre-attempt", 1),
				builder.Step(
					action.ref+"/attempt",
					applyDelegateAttemptKind(action.work),
				),
				statefile.validations(action.ref+"/statefile/post-attempt", 1),
			)
		} else {
			nodes = append(
				nodes,
				builder.Step(action.ref+"/pre-state-dir", operationplan.EffectStepValidateStateDir),
				builder.Step(action.ref+"/attempt", applyDelegateAttemptKind(action.work)),
				builder.Step(action.ref+"/post-state-dir", operationplan.EffectStepValidateStateDir),
			)
		}
		failureRef := action.ref + "/ordinary-failure"
		nodes = append(nodes, builder.Choice(
			action.ref+"/outcome",
			builder.Step(action.ref+"/success", operationplan.EffectStepNoOp),
			builder.Trigger(
				failureRef,
				builder.Step(action.ref+"/ordinary", operationplan.EffectStepNoOp),
			),
			builder.Step(action.ref+"/terminal", operationplan.EffectStepTerminal),
		))
		ordinaryFailures = append(ordinaryFailures, failureRef)
	}
	if persist {
		if len(delegates) > (math.MaxInt-3)/2 {
			if statefile.err == nil {
				statefile.err = fmt.Errorf("apply delegate schedule overflows")
			}
			return operationplan.EffectSequence()
		}
		nodes = append(
			nodes,
			statefile.validations("apply/delegates/statefile/pre-persistence", 1),
			statefile.publications("apply/delegates/statefile/persistence", 1),
			statefile.validations("apply/delegates/statefile/post-persistence", 1),
			compileApplyFailFastChoice(builder, "apply/delegates/persistence-outcome"),
		)
	}
	for _, failureRef := range ordinaryFailures {
		nodes = append(nodes, builder.Conditional(
			failureRef,
			builder.Step(failureRef+"/terminal", operationplan.EffectStepTerminal),
		))
	}
	return operationplan.EffectSequence(nodes...)
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

type applyStatefileSchedule struct {
	builder *operationplan.EffectStructureBuilder
	bound   bool
	err     error
}

func (schedule *applyStatefileSchedule) ensure(prefix string) operationplan.EffectNode {
	if schedule == nil || schedule.builder == nil {
		return operationplan.EffectSequence()
	}
	if !schedule.bound {
		schedule.bound = true
		return schedule.builder.Choice(
			"apply/statefile/initial-authority",
			schedule.builder.Step(
				"apply/statefile/bind",
				operationplan.EffectStepBindDescendant,
			),
			schedule.builder.Step(
				"apply/statefile/validate-existing",
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
) []applyCarrierScheduleFact {
	works := carrierWorks(actions)
	result := make([]applyCarrierScheduleFact, 0, len(actions))
	for index := range actions {
		result = append(result, applyCarrierScheduleFact{
			ref:  applyOrdinalScheduleReference("apply/carrier-removal", index),
			work: works[index],
		})
	}
	return result
}

func sameApplyDemand(left operationplan.Demand, right operationplan.Demand) bool {
	return left.EnsureCalls() == right.EnsureCalls() &&
		left.BarrierValidationCalls() == right.BarrierValidationCalls() &&
		left.StateDirValidationCalls() == right.StateDirValidationCalls() &&
		left.DescendantBindings() == right.DescendantBindings() &&
		left.DescendantValidations() == right.DescendantValidations() &&
		left.DescendantFileCommits() == right.DescendantFileCommits()
}
