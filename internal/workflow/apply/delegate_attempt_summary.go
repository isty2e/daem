package apply

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	lock "github.com/isty2e/daem/internal/realization/lock"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type delegateAttemptKey struct {
	subject topology.SubjectID
	target  string
	scope   string
}

type postAttemptSummary struct {
	observation   observerelation.ObservationSummary
	postcondition observerelation.PostconditionSummary
}

func defaultPostAttemptSummaries(
	locked lock.File,
	attempts []delegate.AttemptRecord,
) map[delegateAttemptKey]postAttemptSummary {
	summaries := make(map[delegateAttemptKey]postAttemptSummary, len(attempts))
	for _, attempt := range attempts {
		summaries[delegateAttemptKeyForAttempt(attempt)] = defaultPostAttemptSummary(
			attemptHasAggregateProjection(locked, attempt),
		)
	}
	return summaries
}

func delegateAttemptResults(
	attempts []delegate.AttemptRecord,
	summaries map[delegateAttemptKey]postAttemptSummary,
) ([]DelegateAttemptResult, error) {
	results := make([]DelegateAttemptResult, 0, len(attempts))
	for _, attempt := range attempts {
		summary, ok := summaries[delegateAttemptKeyForAttempt(attempt)]
		if !ok {
			summary = defaultPostAttemptSummary(false)
		}
		result, err := newDelegateAttemptResult(attempt, summary.observation, summary.postcondition)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func postAttemptSummaries(
	ctx context.Context,
	paths daempaths.Paths,
	locked lock.File,
	selection targetselection.Selection,
	current durable.Snapshot,
	attempts []delegate.AttemptRecord,
) map[delegateAttemptKey]postAttemptSummary {
	summaries := defaultPostAttemptSummaries(locked, attempts)
	subjects := aggregateProjectionAttemptSubjects(locked, attempts)
	if len(subjects) == 0 {
		return summaries
	}

	observations, err := readiness.ObserveAggregateProjectionSummaries(
		ctx,
		liveobserve.DestinationResolver(destinationResolver(paths).Resolve),
		locked,
		selection,
		current,
		aggregatecodec.Catalog(),
		subjects,
	)
	if err != nil {
		markAggregateProjectionSummariesUnknown(summaries, locked, attempts)
		return summaries
	}
	for _, observation := range observations {
		key := delegateAttemptKey{
			subject: observation.Subject(),
			target:  string(observation.Target()),
			scope:   string(observation.Scope()),
		}
		summary, ok := summaries[key]
		if !ok {
			continue
		}
		summary.observation = observation.Current()
		summary.postcondition = observerelation.PostconditionNotObserved
		summaries[key] = summary
	}
	return summaries
}

func defaultPostAttemptSummary(observableAggregate bool) postAttemptSummary {
	if observableAggregate {
		return postAttemptSummary{
			observation:   observerelation.ObservationUnknown,
			postcondition: observerelation.PostconditionNotObserved,
		}
	}
	return postAttemptSummary{
		observation:   observerelation.ObservationNotObserved,
		postcondition: observerelation.PostconditionNotObserved,
	}
}

func markAggregateProjectionSummariesUnknown(
	summaries map[delegateAttemptKey]postAttemptSummary,
	locked lock.File,
	attempts []delegate.AttemptRecord,
) {
	for _, attempt := range attempts {
		if !attemptHasAggregateProjection(locked, attempt) {
			continue
		}
		key := delegateAttemptKeyForAttempt(attempt)
		summaries[key] = postAttemptSummary{
			observation:   observerelation.ObservationUnknown,
			postcondition: observerelation.PostconditionNotObserved,
		}
	}
}

func aggregateProjectionAttemptSubjects(
	locked lock.File,
	attempts []delegate.AttemptRecord,
) []topology.SubjectID {
	seen := make(map[topology.SubjectID]struct{}, len(attempts))
	subjects := make([]topology.SubjectID, 0, len(attempts))
	for _, attempt := range attempts {
		if !attemptHasAggregateProjection(locked, attempt) {
			continue
		}
		subject := attempt.Subject()
		if _, duplicate := seen[subject]; duplicate {
			continue
		}
		seen[subject] = struct{}{}
		subjects = append(subjects, subject)
	}
	return subjects
}

func attemptHasAggregateProjection(
	locked lock.File,
	attempt delegate.AttemptRecord,
) bool {
	contract, present := locked.Locked.Subject(attempt.Subject())
	if !present {
		return false
	}
	_, aggregateProjection, err := contract.ManagedAggregateContribution()
	return err == nil && aggregateProjection
}

func delegateAttemptKeyForAttempt(attempt delegate.AttemptRecord) delegateAttemptKey {
	subject := attempt.Subject()
	return delegateAttemptKey{
		subject: subject,
		target:  attempt.Target(),
		scope:   attempt.Scope(),
	}
}
