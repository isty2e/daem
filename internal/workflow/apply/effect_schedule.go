package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

// applyForwardEffectSchedule is the Apply-owned structural reservation shadow.
// It proves complete pre-effect demand and provider-suffix equivalence while the
// existing scalar authorities still consume runtime checkpoints. Inner route,
// delegate, rollback, and cleanup settlement remains owner-local rather than
// being interpreted by this value.
type applyForwardEffectSchedule struct {
	full  operationplan.EffectStructure
	final operationplan.EffectStructure
}

func requireEquivalentProviderFinalSchedule(
	reserved operationplan.EffectStructure,
	current commandPlan,
	providerActions []reconcile.RelationAction,
) error {
	applyInput, err := applyEffectInput(current)
	if err != nil {
		return err
	}
	currentSchedule, err := compileApplyForwardEffectSchedule(
		current,
		providerActions,
		applyInput,
	)
	if err != nil {
		return err
	}
	if !reserved.Equal(currentSchedule.final) {
		return fmt.Errorf("reserved and current final apply effect schedules differ")
	}
	return nil
}

func compileApplyForwardEffectSchedule(
	current commandPlan,
	providerActions []reconcile.RelationAction,
	applyInput execute.ApplyInput,
) (applyForwardEffectSchedule, error) {
	projectRetirements, globalRetirements, err := stateOnlyCarrierClaimRetirements(
		current.assessment.Reconciliation.CarrierAbsences(),
	)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	projectAdoptions, globalAdoptions, err := stateOnlyCarrierClaimAdoptions(
		current.assessment.CurrentState,
		current.assessment.Reconciliation.CarrierAdoptions(),
	)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	orderClasses, err := admittedOrderClassFacts(current)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	mayReclassifyOrder := relationOrderMayReclassifyBeforeExecution(
		providerActions,
		nonProviderRelationActions(current),
		current.assessment.Reconciliation.CarrierAbsences(),
	)

	var builder operationplan.EffectStructureBuilder
	providerNode, err := applyProviderScheduleNode(&builder, current, providerActions)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	finalNode, err := applyFinalScheduleNode(
		&builder,
		current,
		applyInput,
		projectRetirements,
		globalRetirements,
		projectAdoptions,
		globalAdoptions,
		orderClasses,
		mayReclassifyOrder,
	)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	finalPhase := builder.ForwardPhase("apply/final", finalNode)
	finalStructure, err := builder.Compile(finalPhase)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}

	fullNodes := make([]operationplan.EffectNode, 0, 5)
	if len(providerActions) != 0 {
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
	return applyForwardEffectSchedule{full: fullStructure, final: finalStructure}, nil
}

func applyProviderScheduleNode(
	builder *operationplan.EffectStructureBuilder,
	current commandPlan,
	actions []reconcile.RelationAction,
) (operationplan.EffectNode, error) {
	nodes := make([]operationplan.EffectNode, 0, len(actions))
	for index, action := range actions {
		ref, err := applyScheduleReference("apply/provider/route", index, relationFingerprintRows(
			[]reconcile.RelationAction{action},
		))
		if err != nil {
			return operationplan.EffectNode{}, err
		}
		work := routeWorks(current.assessment.CurrentState, []reconcile.RelationAction{action})[0]
		nodes = append(nodes, applyRouteScheduleNode(builder, ref, work, true))
	}
	return operationplan.EffectSequence(nodes...), nil
}

func applyFinalScheduleNode(
	builder *operationplan.EffectStructureBuilder,
	current commandPlan,
	applyInput execute.ApplyInput,
	projectRetirements []durablecarrier.ManagedCarrierClaim,
	globalRetirements []durablecarrier.ManagedCarrierClaim,
	projectAdoptions []durablecarrier.ManagedCarrierClaim,
	globalAdoptions []durablecarrier.ManagedCarrierClaim,
	orderClasses []admittedOrderClass,
	mayReclassifyOrder bool,
) (operationplan.EffectNode, error) {
	effectSegment, err := execute.ApplyEffectSegment(applyInput)
	if err != nil {
		return operationplan.EffectNode{}, err
	}
	effectIdentity, err := applyScheduleReference("apply/effect-input", 0, struct {
		ManagedPaths       []managedPathFingerprintFacts
		Aggregates         []aggregateFingerprintFacts
		Relations          []relationFingerprintFacts
		Owner              ownershipOwnerFingerprintFacts
		Ownership          []ownershipObservationFingerprintFacts
		ProjectRetirements []carrierClaimFingerprintFacts
		ProjectAdoptions   []carrierClaimFingerprintFacts
	}{
		ManagedPaths: managedPathFingerprintRows(
			current.assessment.Reconciliation.ManagedPaths(),
		),
		Aggregates: aggregateFingerprintRows(
			current.assessment.Reconciliation.Aggregates(),
		),
		Relations: relationFingerprintRows(nonProviderRelationActions(current)),
		Owner: ownershipOwnerFingerprintFacts{
			StatefileAuthority: pathAuthorityFingerprintFactsFor(
				current.assessment.Owner.StatefileAuthority(),
			),
			ManifestPath: current.assessment.Owner.ManifestPath(),
		},
		Ownership:          ownershipFingerprintFacts(current.assessment.Ownership),
		ProjectRetirements: carrierClaimScheduleFactsFor(projectRetirements),
		ProjectAdoptions:   carrierClaimScheduleFactsFor(projectAdoptions),
	})
	if err != nil {
		return operationplan.EffectNode{}, err
	}
	nodes := make([]operationplan.EffectNode, 0, 9)
	nodes = append(nodes, builder.Step(effectIdentity, operationplan.EffectStepNoOp))
	if len(projectRetirements) != 0 {
		ref, err := applyScheduleReference(
			"apply/project-claim-retirements",
			0,
			carrierClaimScheduleFactsFor(projectRetirements),
		)
		if err != nil {
			return operationplan.EffectNode{}, err
		}
		nodes = append(nodes, builder.Step(ref, operationplan.EffectStepNoOp))
	}
	if len(projectAdoptions) != 0 {
		ref, err := applyScheduleReference(
			"apply/project-claim-adoptions",
			0,
			carrierClaimScheduleFactsFor(projectAdoptions),
		)
		if err != nil {
			return operationplan.EffectNode{}, err
		}
		nodes = append(nodes, builder.Step(ref, operationplan.EffectStepNoOp))
	}
	nodes = append(nodes, effectSegment)
	if len(globalRetirements) != 0 {
		ref, err := applyScheduleReference(
			"apply/global-claim-retirements",
			0,
			carrierClaimScheduleFactsFor(globalRetirements),
		)
		if err != nil {
			return operationplan.EffectNode{}, err
		}
		nodes = append(nodes, builder.Step(ref, operationplan.EffectStepPersistence))
	}
	carrierNodes, err := applyCarrierRemovalScheduleNodes(
		builder,
		current.assessment.Reconciliation.CarrierAbsences(),
	)
	if err != nil {
		return operationplan.EffectNode{}, err
	}
	nodes = append(nodes, carrierNodes...)
	routeNodes, err := applyFinalRouteScheduleNodes(
		builder,
		current.assessment.CurrentState,
		nonProviderRelationActions(current),
	)
	if err != nil {
		return operationplan.EffectNode{}, err
	}
	nodes = append(nodes, routeNodes...)
	orderNodes, err := applyOrderScheduleNodes(builder, orderClasses, mayReclassifyOrder)
	if err != nil {
		return operationplan.EffectNode{}, err
	}
	nodes = append(nodes, orderNodes...)
	delegateNode, err := applyDelegateScheduleNode(
		builder,
		current.assessment.Reconciliation.Delegates(),
	)
	if err != nil {
		return operationplan.EffectNode{}, err
	}
	nodes = append(nodes, delegateNode)
	if len(globalAdoptions) != 0 {
		ref, err := applyScheduleReference(
			"apply/global-claim-adoptions",
			0,
			carrierClaimScheduleFactsFor(globalAdoptions),
		)
		if err != nil {
			return operationplan.EffectNode{}, err
		}
		nodes = append(nodes, builder.Step(ref, operationplan.EffectStepPersistence))
	}
	return operationplan.EffectSequence(nodes...), nil
}

func applyFinalRouteScheduleNodes(
	builder *operationplan.EffectStructureBuilder,
	current durable.Snapshot,
	actions []reconcile.RelationAction,
) ([]operationplan.EffectNode, error) {
	works := routeWorks(current, actions)
	hostNodes := make([]operationplan.EffectNode, 0, len(actions))
	promotionNodes := make([]operationplan.EffectNode, 0, len(actions)*2+1)
	for index, action := range actions {
		work := works[index]
		ref, err := applyScheduleReference(
			"apply/final/route",
			index,
			relationFingerprintRows([]reconcile.RelationAction{action}),
		)
		if err != nil {
			return nil, err
		}
		switch {
		case work.InvokesHost:
			hostNodes = append(hostNodes, applyRouteScheduleNode(builder, ref, work, false))
		case work.Promotion:
			if len(promotionNodes) == 0 {
				promotionNodes = append(
					promotionNodes,
					builder.Step("apply/final/promotions/forward", operationplan.EffectStepForwardEffect),
				)
			}
			promotionNodes = append(
				promotionNodes,
				builder.Step(ref+"/promotion", operationplan.EffectStepPersistence),
				applyStatefileScheduleNode(builder, ref+"/statefile", 4, 1),
			)
		}
	}
	nodes := make([]operationplan.EffectNode, 0, len(hostNodes)+1)
	if len(promotionNodes) != 0 {
		nodes = append(nodes, operationplan.EffectSequence(promotionNodes...))
	}
	return append(nodes, hostNodes...), nil
}

func applyRouteScheduleNode(
	builder *operationplan.EffectStructureBuilder,
	ref string,
	work operationplan.RouteWork,
	provider bool,
) operationplan.EffectNode {
	nodes := []operationplan.EffectNode{
		builder.Step(ref+"/forward", operationplan.EffectStepForwardEffect),
	}
	switch {
	case work.InvokesHost:
		nodes = append(nodes, builder.Step(ref+"/host", operationplan.EffectStepExternal))
		validations := 7
		if work.Global {
			validations = 10
		}
		nodes = append(nodes, applyStatefileScheduleNode(builder, ref+"/statefile", validations, 4))
	case work.Promotion:
		nodes = append(
			nodes,
			builder.Step(ref+"/promotion", operationplan.EffectStepPersistence),
			applyStatefileScheduleNode(builder, ref+"/statefile", 4, 1),
		)
	case provider:
		nodes = append(nodes, builder.Step(ref+"/noop", operationplan.EffectStepNoOp))
	}
	return operationplan.EffectSequence(nodes...)
}

func applyCarrierRemovalScheduleNodes(
	builder *operationplan.EffectStructureBuilder,
	actions []carrierabsence.Action,
) ([]operationplan.EffectNode, error) {
	works := carrierWorks(actions)
	nodes := make([]operationplan.EffectNode, 0, len(actions))
	for index, action := range actions {
		if !works[index].InvokesHost && !works[index].MutatesDirect && !works[index].VerifiesPending {
			continue
		}
		ref, err := applyScheduleReference(
			"apply/carrier-removal",
			index,
			carrierAbsenceFingerprintRows([]carrierabsence.Action{action}),
		)
		if err != nil {
			return nil, err
		}
		kind := operationplan.EffectStepObservation
		if works[index].InvokesHost {
			kind = operationplan.EffectStepExternal
		} else if works[index].MutatesDirect {
			kind = operationplan.EffectStepPersistence
		}
		nodes = append(nodes, operationplan.EffectSequence(
			builder.Step(ref+"/forward", operationplan.EffectStepForwardEffect),
			builder.Step(ref+"/effect", kind),
			applyStatefileScheduleNode(builder, ref+"/statefile", 8, 3),
		))
	}
	return nodes, nil
}

func applyOrderScheduleNodes(
	builder *operationplan.EffectStructureBuilder,
	classes []admittedOrderClass,
	mayReclassify bool,
) ([]operationplan.EffectNode, error) {
	nodes := make([]operationplan.EffectNode, 0, len(classes))
	for index, class := range classes {
		ref, err := applyScheduleReference("apply/relation-order", index, struct {
			Target      target.Target
			Scope       target.Scope
			ClassID     string
			Fingerprint string
		}{
			Target:      class.target,
			Scope:       class.scope,
			ClassID:     class.classID,
			Fingerprint: class.fingerprint,
		})
		if err != nil {
			return nil, err
		}
		mutating := operationplan.EffectSequence(
			builder.Step(ref+"/forward", operationplan.EffectStepForwardEffect),
			builder.Step(ref+"/external", operationplan.EffectStepExternal),
			builder.Step(ref+"/observation", operationplan.EffectStepObservation),
		)
		switch {
		case mayReclassify:
			nodes = append(nodes, builder.Choice(
				ref+"/choice",
				builder.Step(ref+"/noop", operationplan.EffectStepNoOp),
				mutating,
			))
		case relationOrderMutationRequired(class.decisions):
			nodes = append(nodes, mutating)
		default:
			nodes = append(nodes, builder.Step(ref+"/noop", operationplan.EffectStepNoOp))
		}
	}
	return nodes, nil
}

func applyDelegateScheduleNode(
	builder *operationplan.EffectStructureBuilder,
	actions []reconcile.DelegateAction,
) (operationplan.EffectNode, error) {
	if len(actions) == 0 {
		return operationplan.EffectSequence(), nil
	}
	refs := make([]string, 0, len(actions))
	for index, action := range actions {
		ref, err := applyScheduleReference(
			"apply/delegate",
			index,
			delegateFingerprintRows([]reconcile.DelegateAction{action}),
		)
		if err != nil {
			return operationplan.EffectNode{}, err
		}
		refs = append(refs, ref)
	}
	if !delegateActionsRequireAttemptPersistence(actions) {
		nodes := make([]operationplan.EffectNode, 0, len(actions))
		for _, ref := range refs {
			nodes = append(nodes, operationplan.EffectSequence(
				builder.Step(ref+"/pre-state-dir", operationplan.EffectStepValidateStateDir),
				builder.Step(ref+"/attempt", operationplan.EffectStepExternal),
				builder.Step(ref+"/post-state-dir", operationplan.EffectStepValidateStateDir),
			))
		}
		return operationplan.EffectSequence(nodes...), nil
	}
	if len(actions) > (math.MaxInt-3)/2 {
		return operationplan.EffectNode{}, fmt.Errorf("apply delegate schedule overflows")
	}
	nodes := make([]operationplan.EffectNode, 0, len(actions)+2)
	nodes = append(nodes, builder.Step("apply/delegates/forward", operationplan.EffectStepForwardEffect))
	for _, ref := range refs {
		nodes = append(nodes, builder.Step(ref+"/attempt", operationplan.EffectStepExternal))
	}
	nodes = append(nodes, applyStatefileScheduleNode(
		builder,
		"apply/delegates/statefile",
		len(actions)*2+3,
		1,
	))
	return operationplan.EffectSequence(nodes...), nil
}

func applyStatefileScheduleNode(
	builder *operationplan.EffectStructureBuilder,
	prefix string,
	validations int,
	commits int,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Repeat(
			validations,
			builder.Step(prefix+"/validate", operationplan.EffectStepValidateDescendant),
		),
		builder.Repeat(
			commits,
			builder.Step(prefix+"/publish", operationplan.EffectStepPublishDescendant),
		),
	)
}

func carrierClaimScheduleFactsFor(
	claims []durablecarrier.ManagedCarrierClaim,
) []carrierClaimFingerprintFacts {
	result := make([]carrierClaimFingerprintFacts, 0, len(claims))
	for _, claim := range claims {
		result = append(result, carrierClaimFingerprintFact(claim))
	}
	return result
}

func applyScheduleReference(prefix string, index int, value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode apply effect schedule identity: %w", err)
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%s/%06d/%s", prefix, index, hex.EncodeToString(digest[:])), nil
}
