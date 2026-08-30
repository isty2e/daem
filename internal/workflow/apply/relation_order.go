package apply

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/configrelation"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
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

// PublicDetail derives path-neutral prose from a closed execution outcome.
func (result RelationOrderExecutionResult) PublicDetail() string {
	if result.detail == "" {
		return ""
	}
	switch result.outcome {
	case RelationOrderFailed:
		return "extension order update failed"
	case RelationOrderNotAttempted:
		return "extension order update was not attempted"
	default:
		return ""
	}
}

// RelationOrderRiskDelta is one immutable physical-sequence fragment containing
// only precedence changes absent from the authorized baseline.
type RelationOrderRiskDelta struct {
	target         target.Target
	scope          target.Scope
	classID        hostrelation.OrderClassID
	sequenceID     hostrelation.PhysicalSequenceID
	runtimeMeaning hostrelation.RuntimeMeaning
	changes        []observerelation.PrecedenceChange
}

func newRelationOrderRiskDelta(
	decision reconcile.RelationOrderDecision,
	changes []observerelation.PrecedenceChange,
) RelationOrderRiskDelta {
	return RelationOrderRiskDelta{
		target:         decision.Target(),
		scope:          decision.Scope(),
		classID:        decision.ClassID(),
		sequenceID:     decision.SequenceID(),
		runtimeMeaning: decision.RuntimeMeaning(),
		changes: append(
			[]observerelation.PrecedenceChange(nil),
			changes...,
		),
	}
}

func (delta RelationOrderRiskDelta) Target() target.Target { return delta.target }

func (delta RelationOrderRiskDelta) Scope() target.Scope { return delta.scope }

func (delta RelationOrderRiskDelta) ClassID() hostrelation.OrderClassID {
	return delta.classID
}

func (delta RelationOrderRiskDelta) SequenceID() hostrelation.PhysicalSequenceID {
	return delta.sequenceID
}

func (delta RelationOrderRiskDelta) RuntimeMeaning() hostrelation.RuntimeMeaning {
	return delta.runtimeMeaning
}

func (delta RelationOrderRiskDelta) PrecedenceChanges() []observerelation.PrecedenceChange {
	return append([]observerelation.PrecedenceChange(nil), delta.changes...)
}

func (delta RelationOrderRiskDelta) clone() RelationOrderRiskDelta {
	delta.changes = delta.PrecedenceChanges()
	return delta
}

// RelationOrderRiskExpansion is the exact bounded post-route risk delta that
// introduces foreign-precedence changes absent from the authorized plan.
type RelationOrderRiskExpansion struct {
	deltas []RelationOrderRiskDelta
}

func (expansion RelationOrderRiskExpansion) Deltas() []RelationOrderRiskDelta {
	result := make([]RelationOrderRiskDelta, len(expansion.deltas))
	for index, delta := range expansion.deltas {
		result[index] = delta.clone()
	}
	return result
}

func (expansion RelationOrderRiskExpansion) AddedRiskCount() int {
	count := 0
	for _, delta := range expansion.deltas {
		count += len(delta.changes)
	}
	return count
}

// RelationOrderRiskAuthorizer obtains renewed consent for a post-carrier risk
// expansion. Returning false declines the updated plan.
type RelationOrderRiskAuthorizer func(
	context.Context,
	RelationOrderRiskExpansion,
) (bool, error)

type relationOrderRunResult struct {
	reconciliation  reconcile.Result
	results         []RelationOrderExecutionResult
	actionCount     int
	updated         bool
	planFingerprint mutation.OperationFingerprint
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
		fingerprint, err := remainingExecutionFingerprint(reconciliation)
		return relationOrderRunResult{
			reconciliation:  reconciliation,
			planFingerprint: fingerprint,
		}, err
	}
	if err := options.orderRiskBaseline.validate(); err != nil {
		return relationOrderRunResult{}, err
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
		selectedTarget, capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapabilityForClass(
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
	result.planFingerprint, err = remainingExecutionFingerprint(fresh)
	if err != nil {
		return result, err
	}
	if err := rejectBlockedRelationOrders(fresh); err != nil {
		return result, err
	}

	expansion := options.orderRiskBaseline.expansion(freshDecisions)
	if expansion.AddedRiskCount() != 0 {
		if options.RelationOrderRiskAuthorizer == nil {
			return result, fmt.Errorf(
				"%w: %d newly discovered managed/foreign precedence changes; rerun interactively or inspect a fresh dry-run",
				ErrRelationOrderRiskExpansion,
				expansion.AddedRiskCount(),
			)
		}
		authorized, err := options.RelationOrderRiskAuthorizer(ctx, expansion)
		if err != nil {
			return result, fmt.Errorf("authorize updated extension order plan: %w", err)
		}
		if !authorized {
			return result, ErrRelationOrderNotAuthorized
		}
		actualFingerprint, fingerprintErr := remainingExecutionFingerprint(fresh)
		if fingerprintErr != nil {
			return result, fingerprintErr
		}
		if err := options.executionGuard.requirePlanCurrent(
			ctx,
			result.planFingerprint,
			actualFingerprint,
			"renewed extension order authorization",
		); err != nil {
			return result, err
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
		options.markAttempted()
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
		if executeErr != nil {
			executeErr = relationOrderExecutionError{cause: executeErr}
		}
		closeErr := bound.Close()
		declarationErr := options.executionGuard.requireDeclarationsCurrent(
			ctx,
			"after extension order execution",
		)
		result.actionCount += changed
		if executeErr != nil || closeErr != nil || declarationErr != nil {
			return result, errors.Join(executeErr, closeErr, declarationErr)
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

type relationOrderExecutionError struct {
	cause error
}

func (err relationOrderExecutionError) Error() string {
	return "extension order execution failed"
}

func (err relationOrderExecutionError) Unwrap() error {
	return err.cause
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
	managedSubject    topology.SubjectID
	foreignIdentity   hostrelation.HostLoadIdentity
	managedWasBefore  bool
	managedWillBefore bool
}

// relationOrderRiskBaseline is the immutable set of precedence risks accepted
// with the original private apply plan. Its zero value is invalid; a
// constructed empty baseline authorizes no risks.
type relationOrderRiskBaseline struct {
	authorized map[relationOrderRiskKey]struct{}
}

func newRelationOrderRiskBaseline(
	decisions []reconcile.RelationOrderDecision,
) relationOrderRiskBaseline {
	authorized := make(map[relationOrderRiskKey]struct{})
	for _, decision := range decisions {
		for _, change := range decision.PrecedenceChanges() {
			authorized[relationOrderRiskKey{
				classID:           decision.ClassID(),
				sequenceID:        decision.SequenceID(),
				managedSubject:    change.ManagedSubject(),
				foreignIdentity:   change.ForeignIdentity(),
				managedWasBefore:  change.ManagedWasBefore(),
				managedWillBefore: change.ManagedWillBeBefore(),
			}] = struct{}{}
		}
	}
	return relationOrderRiskBaseline{authorized: authorized}
}

func (baseline relationOrderRiskBaseline) validate() error {
	if baseline.authorized == nil {
		return fmt.Errorf("relation order risk baseline is required")
	}
	return nil
}

func (baseline relationOrderRiskBaseline) expansion(
	fresh []reconcile.RelationOrderDecision,
) RelationOrderRiskExpansion {
	var deltas []RelationOrderRiskDelta
	for _, decision := range fresh {
		var added []observerelation.PrecedenceChange
		for _, change := range decision.PrecedenceChanges() {
			key := relationOrderRiskKey{
				classID:           decision.ClassID(),
				sequenceID:        decision.SequenceID(),
				managedSubject:    change.ManagedSubject(),
				foreignIdentity:   change.ForeignIdentity(),
				managedWasBefore:  change.ManagedWasBefore(),
				managedWillBefore: change.ManagedWillBeBefore(),
			}
			if _, present := baseline.authorized[key]; present {
				continue
			}
			added = append(added, change)
		}
		if len(added) != 0 {
			deltas = append(deltas, newRelationOrderRiskDelta(decision, added))
		}
	}
	slices.SortFunc(deltas, func(left, right RelationOrderRiskDelta) int {
		if left.classID != right.classID {
			return cmp.Compare(left.classID, right.classID)
		}
		return cmp.Compare(left.sequenceID, right.sequenceID)
	})
	return RelationOrderRiskExpansion{deltas: deltas}
}
