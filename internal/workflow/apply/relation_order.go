package apply

import (
	"context"
	"errors"
	"fmt"
	"slices"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/configrelation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

var (
	ErrRelationOrderRiskExpansion = errors.New("extension order risk expanded after carrier changes")
	ErrRelationOrderNotAuthorized = errors.New("updated extension order plan was not authorized")
)

// RelationOrderOutcome is the closed execution outcome for one physical
// sequence in the final post-carrier plan.
type RelationOrderOutcome string

const (
	RelationOrderExact        RelationOrderOutcome = "exact"
	RelationOrderConverged    RelationOrderOutcome = "converged"
	RelationOrderFailed       RelationOrderOutcome = "failed"
	RelationOrderNotAttempted RelationOrderOutcome = "not_attempted"
)

// RelationOrderExecutionResult records one final physical-sequence outcome.
type RelationOrderExecutionResult struct {
	target     target.Target
	scope      target.Scope
	classID    hostrelation.OrderClassID
	sequenceID hostrelation.PhysicalSequenceID
	outcome    RelationOrderOutcome
	changed    bool
	detail     string
}

func (result RelationOrderExecutionResult) Target() target.Target { return result.target }
func (result RelationOrderExecutionResult) Scope() target.Scope   { return result.scope }
func (result RelationOrderExecutionResult) ClassID() hostrelation.OrderClassID {
	return result.classID
}

func (result RelationOrderExecutionResult) SequenceID() hostrelation.PhysicalSequenceID {
	return result.sequenceID
}
func (result RelationOrderExecutionResult) Outcome() RelationOrderOutcome { return result.outcome }
func (result RelationOrderExecutionResult) Changed() bool                 { return result.changed }
func (result RelationOrderExecutionResult) Detail() string                { return result.detail }

// RelationOrderRiskExpansion is the bounded post-route plan fragment that
// introduces foreign-precedence changes not present in the authorized plan.
type RelationOrderRiskExpansion struct {
	decisions      []reconcile.RelationOrderDecision
	addedRiskCount int
}

func (expansion RelationOrderRiskExpansion) Decisions() []reconcile.RelationOrderDecision {
	return append([]reconcile.RelationOrderDecision(nil), expansion.decisions...)
}

func (expansion RelationOrderRiskExpansion) AddedRiskCount() int {
	return expansion.addedRiskCount
}

// RelationOrderRiskAuthorizer obtains renewed consent for a post-carrier risk
// expansion. Returning false declines the updated plan.
type RelationOrderRiskAuthorizer func(
	context.Context,
	RelationOrderRiskExpansion,
) (bool, error)

type relationOrderRunResult struct {
	reconciliation reconcile.Result
	results        []RelationOrderExecutionResult
	actionCount    int
	updated        bool
}

type observedOrderClass struct {
	observation relationhost.OrderObservation
	decisions   []reconcile.RelationOrderDecision
}

func runRelationOrderConvergence(
	ctx context.Context,
	paths daempaths.Paths,
	locked lock.File,
	reconciliation reconcile.Result,
	options runOptions,
) (relationOrderRunResult, error) {
	initial := reconciliation.RelationOrders()
	if len(initial) == 0 {
		return relationOrderRunResult{reconciliation: reconciliation}, nil
	}

	selectedClasses := make(map[hostrelation.OrderClassID]struct{})
	for _, decision := range initial {
		selectedClasses[decision.ClassID()] = struct{}{}
	}
	observed := make([]observedOrderClass, 0, len(selectedClasses))
	freshDecisions := make([]reconcile.RelationOrderDecision, 0, len(initial))
	matchedClasses := 0
	for _, constraint := range locked.Locked.OrderConstraints() {
		if _, selected := selectedClasses[constraint.ClassID()]; !selected {
			continue
		}
		matchedClasses++
		selectedTarget, capability, admitted := profile.ExtensionOrderCapabilityForClass(
			constraint.ClassID(),
		)
		if !admitted {
			return relationOrderRunResult{}, fmt.Errorf(
				"locked extension order class %q has no unique profile owner",
				constraint.ClassID(),
			)
		}
		observation, err := relationhost.ObserveOrder(relationhost.OrderInput{
			Paths:      paths,
			Lockfile:   locked,
			Constraint: constraint,
		})
		if err != nil {
			for _, sequenceID := range capability.PhysicalSequenceIDs() {
				blocked, blockErr := reconcile.NewBlockedRelationOrderDecision(
					reconcile.BlockedRelationOrderDecisionInput{
						Target:     selectedTarget,
						Scope:      capability.Scope(),
						Constraint: constraint,
						SequenceID: sequenceID,
						Reason: reconcile.RelationOrderObservationFailureReason(
							err,
						),
						Detail: err.Error(),
					},
				)
				if blockErr != nil {
					return relationOrderRunResult{}, blockErr
				}
				freshDecisions = append(freshDecisions, blocked)
			}
			continue
		}
		classDecisions, err := readiness.DecideExtensionOrderObservation(
			selectedTarget,
			capability,
			constraint,
			observation,
			nil,
			nil,
		)
		if err != nil {
			return relationOrderRunResult{}, err
		}
		freshDecisions = append(freshDecisions, classDecisions...)
		observed = append(observed, observedOrderClass{
			observation: observation,
			decisions:   classDecisions,
		})
	}
	if matchedClasses != len(selectedClasses) {
		return relationOrderRunResult{}, fmt.Errorf(
			"post-carrier extension order matched %d locked classes, want %d",
			matchedClasses,
			len(selectedClasses),
		)
	}
	fresh, err := reconciliation.WithRelationOrders(freshDecisions)
	if err != nil {
		return relationOrderRunResult{}, err
	}
	result := relationOrderRunResult{
		reconciliation: fresh,
		results:        initialOrderExecutionResults(freshDecisions),
		updated:        true,
	}
	if err := rejectBlockedRelationOrders(fresh); err != nil {
		return result, err
	}

	expansion := expandedOrderRisk(initial, freshDecisions)
	if expansion.addedRiskCount != 0 {
		if options.RelationOrderRiskAuthorizer == nil {
			return result, fmt.Errorf(
				"%w: %d newly discovered managed/foreign precedence changes; rerun interactively or inspect a fresh dry-run",
				ErrRelationOrderRiskExpansion,
				expansion.addedRiskCount,
			)
		}
		authorized, err := options.RelationOrderRiskAuthorizer(ctx, expansion)
		if err != nil {
			return result, fmt.Errorf("authorize updated extension order plan: %w", err)
		}
		if !authorized {
			return result, ErrRelationOrderNotAuthorized
		}
	}

	resultBySequence := make(map[hostrelation.PhysicalSequenceID]int, len(result.results))
	for index := range result.results {
		resultBySequence[result.results[index].sequenceID] = index
	}
	for _, class := range observed {
		if !relationOrderMutationRequired(class.decisions) {
			continue
		}
		plan, err := configrelation.NewOrderPlan(class.observation)
		if err != nil {
			return result, err
		}
		authority, err := plan.PhysicalAuthority()
		if err != nil {
			return result, err
		}
		if options.validateBeforeEffects == nil {
			return result, fmt.Errorf("extension order effect validation is required")
		}
		if err := options.validateBeforeEffects(ctx, authority); err != nil {
			return result, err
		}
		bound, err := plan.Bind(options.projectRoot, paths.ManifestRoot)
		if err != nil {
			return result, err
		}
		changed, executeErr := bound.Execute(
			ctx,
			storagecommit.Adapter{},
			func(event configrelation.OrderSequenceEvent) {
				recordRelationOrderEvent(
					event,
					class.decisions,
					result.results,
					resultBySequence,
					options.ExecuteEvents,
				)
			},
		)
		closeErr := bound.Close()
		result.actionCount += changed
		if executeErr != nil || closeErr != nil {
			return result, errors.Join(executeErr, closeErr)
		}
		for _, decision := range class.decisions {
			index := resultBySequence[decision.SequenceID()]
			if result.results[index].outcome == RelationOrderNotAttempted {
				return result, fmt.Errorf(
					"extension order sequence %q produced no execution outcome",
					decision.SequenceID(),
				)
			}
		}
	}
	return result, nil
}

func initialOrderExecutionResults(
	decisions []reconcile.RelationOrderDecision,
) []RelationOrderExecutionResult {
	results := make([]RelationOrderExecutionResult, 0, len(decisions))
	for _, decision := range decisions {
		outcome := RelationOrderNotAttempted
		if decision.Kind() == reconcile.OrderExact {
			outcome = RelationOrderExact
		}
		results = append(results, RelationOrderExecutionResult{
			target:     decision.Target(),
			scope:      decision.Scope(),
			classID:    decision.ClassID(),
			sequenceID: decision.SequenceID(),
			outcome:    outcome,
		})
	}
	return results
}

func relationOrderMutationRequired(decisions []reconcile.RelationOrderDecision) bool {
	for _, decision := range decisions {
		if decision.RequiresMutation() {
			return true
		}
	}
	return false
}

func recordRelationOrderEvent(
	event configrelation.OrderSequenceEvent,
	decisions []reconcile.RelationOrderDecision,
	results []RelationOrderExecutionResult,
	resultBySequence map[hostrelation.PhysicalSequenceID]int,
	sink execute.EventSink,
) {
	index, present := resultBySequence[event.SequenceID]
	if !present {
		return
	}
	decision, present := relationOrderDecisionForSequence(decisions, event.SequenceID)
	if !present {
		return
	}
	facts := &execute.RelationOrderEventFacts{
		Target:     decision.Target(),
		Scope:      decision.Scope(),
		SequenceID: event.SequenceID,
		Changed:    event.Changed,
	}
	switch event.Kind {
	case configrelation.OrderSequenceStarted:
		sink.Emit(execute.Event{
			Kind: execute.EventRelationOrderStarted, Stage: execute.EventStageRelationOrder,
			RelationOrder: facts,
		})
	case configrelation.OrderSequenceDone:
		if decision.RequiresMutation() {
			results[index].outcome = RelationOrderConverged
		} else {
			results[index].outcome = RelationOrderExact
		}
		results[index].changed = event.Changed
		sink.Emit(execute.Event{
			Kind: execute.EventRelationOrderDone, Stage: execute.EventStageRelationOrder,
			RelationOrder: facts,
		})
	case configrelation.OrderSequenceFailed:
		results[index].outcome = RelationOrderFailed
		results[index].changed = event.Changed
		if event.Err != nil {
			results[index].detail = event.Err.Error()
		}
		sink.Emit(execute.Event{
			Kind: execute.EventRelationOrderFailed, Stage: execute.EventStageRelationOrder,
			RelationOrder: facts, Err: event.Err,
		})
	}
}

func relationOrderDecisionForSequence(
	decisions []reconcile.RelationOrderDecision,
	sequenceID hostrelation.PhysicalSequenceID,
) (reconcile.RelationOrderDecision, bool) {
	for _, decision := range decisions {
		if decision.SequenceID() == sequenceID {
			return decision, true
		}
	}
	return reconcile.RelationOrderDecision{}, false
}

type relationOrderRiskKey struct {
	classID           hostrelation.OrderClassID
	sequenceID        hostrelation.PhysicalSequenceID
	managedSubject    string
	foreignIdentity   string
	managedWasBefore  bool
	managedWillBefore bool
}

func expandedOrderRisk(
	initial []reconcile.RelationOrderDecision,
	fresh []reconcile.RelationOrderDecision,
) RelationOrderRiskExpansion {
	authorized := make(map[relationOrderRiskKey]struct{})
	for _, decision := range initial {
		for _, change := range decision.PrecedenceChanges() {
			authorized[relationOrderRiskKey{
				classID:           decision.ClassID(),
				sequenceID:        decision.SequenceID(),
				managedSubject:    change.ManagedSubject().String(),
				foreignIdentity:   string(change.ForeignIdentity()),
				managedWasBefore:  change.ManagedWasBefore(),
				managedWillBefore: change.ManagedWillBeBefore(),
			}] = struct{}{}
		}
	}
	var expanded []reconcile.RelationOrderDecision
	count := 0
	for _, decision := range fresh {
		addedForDecision := false
		for _, change := range decision.PrecedenceChanges() {
			key := relationOrderRiskKey{
				classID:           decision.ClassID(),
				sequenceID:        decision.SequenceID(),
				managedSubject:    change.ManagedSubject().String(),
				foreignIdentity:   string(change.ForeignIdentity()),
				managedWasBefore:  change.ManagedWasBefore(),
				managedWillBefore: change.ManagedWillBeBefore(),
			}
			if _, present := authorized[key]; present {
				continue
			}
			count++
			addedForDecision = true
		}
		if addedForDecision {
			expanded = append(expanded, decision)
		}
	}
	slices.SortFunc(expanded, func(left, right reconcile.RelationOrderDecision) int {
		return left.Compare(right)
	})
	return RelationOrderRiskExpansion{
		decisions:      expanded,
		addedRiskCount: count,
	}
}
