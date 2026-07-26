package durable

import (
	"fmt"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
)

func validateUniqueDelegateAttempts(values []durableattempt.DelegateAttempt) error {
	seen := make(map[durableattempt.DelegateAttemptKey]struct{}, len(values))
	for index, attempt := range values {
		if err := attempt.Validate(); err != nil {
			return fmt.Errorf("delegate attempt[%d]: %w", index, err)
		}
		key := attempt.SemanticKey()
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("delegate attempt[%d]: duplicate semantic key", index)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateUniqueHostRouteAttempts(values []durableattempt.HostRouteAttempt) error {
	seen := make(map[durableattempt.HostRouteAttemptKey]struct{}, len(values))
	for index, attempt := range values {
		if err := attempt.Validate(); err != nil {
			return fmt.Errorf("host route attempt[%d]: %w", index, err)
		}
		key := attempt.SemanticKey()
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("host route attempt[%d]: duplicate semantic key", index)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func compareManagedPath(left ManagedPathState, right ManagedPathState) int {
	return compareStrings(
		string(left.scope), string(right.scope),
		left.subject.String(), right.subject.String(),
		string(left.destination), string(right.destination),
	)
}

func compareManagedAggregate(left ManagedAggregateState, right ManagedAggregateState) int {
	return compareStrings(left.subject.String(), right.subject.String())
}

func compareStrings(values ...string) int {
	for index := 0; index < len(values); index += 2 {
		switch {
		case values[index] < values[index+1]:
			return -1
		case values[index] > values[index+1]:
			return 1
		}
	}
	return 0
}
